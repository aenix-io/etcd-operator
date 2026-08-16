/*
Copyright 2023 Timofey Larkin.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RunRestore must be a no-op when the data dir is already initialized (a
// member/ directory exists), so a Pod restart after first boot leaves the
// live data untouched and never re-downloads a snapshot.
func TestRunRestore_NoOpOnInitializedDataDir(t *testing.T) {
	dataDir := t.TempDir()
	memberDir := filepath.Join(dataDir, "member")
	if err := os.MkdirAll(filepath.Join(memberDir, "snap"), 0o755); err != nil {
		t.Fatalf("seed member dir: %v", err)
	}
	sentinel := filepath.Join(memberDir, "snap", "db")
	if err := os.WriteFile(sentinel, []byte("live data"), 0o644); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}

	t.Setenv(envDataDir, dataDir)
	// No destination env set: if RunRestore tried to fetch a snapshot it
	// would fail on loadDestination. The no-op must return before that.

	if err := RunRestore(context.Background()); err != nil {
		t.Fatalf("RunRestore on initialized data dir = %v, want nil (no-op)", err)
	}

	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel after restore: %v", err)
	}
	if string(got) != "live data" {
		t.Errorf("sentinel mutated to %q; live data must be untouched", got)
	}
}

// A restore S3 source needs the EXACT object key. An empty key must fail early
// with a clear message rather than issuing a GetObject for "" deep in the run.
func TestRunRestore_S3EmptyKeyFailsEarly(t *testing.T) {
	dataDir := t.TempDir() // empty: no member/ dir, so the no-op gate is passed

	t.Setenv(envDataDir, dataDir)
	t.Setenv(envDestKind, "s3")
	t.Setenv(envS3Endpoint, "https://s3.example.com")
	t.Setenv(envS3Bucket, "etcd")
	t.Setenv(envS3Key, "") // the bug: empty exact key

	err := RunRestore(context.Background())
	if err == nil {
		t.Fatal("RunRestore with empty S3 key = nil, want error")
	}
	if !strings.Contains(err.Error(), "exact S3 object key") {
		t.Errorf("error did not mention the exact object key requirement: %v", err)
	}
}

// A PVC restore source with an empty subPath resolves to the mount directory;
// without a guard etcdutl would fail opaquely trying to read a directory.
func TestRunRestore_PVCEmptySubPathFailsEarly(t *testing.T) {
	dataDir := t.TempDir() // empty: no member/ dir
	mount := t.TempDir()

	t.Setenv(envDataDir, dataDir)
	t.Setenv(envDestKind, "pvc")
	t.Setenv(envPVCMountPath, mount)
	t.Setenv(envPVCSubPath, "") // the bug: no exact file path

	err := RunRestore(context.Background())
	if err == nil {
		t.Fatal("RunRestore with empty PVC subPath = nil, want error")
	}
	if !strings.Contains(err.Error(), "exact snapshot file path") {
		t.Errorf("error did not mention the exact file-path requirement: %v", err)
	}
}

func TestEnsureRestoreSpace(t *testing.T) {
	dir := t.TempDir()

	if n, err := availableBytes(dir); err != nil || n == 0 {
		t.Fatalf("availableBytes(%s) = %d, %v; want > 0, nil", dir, n, err)
	}

	// A zero-byte snapshot always fits.
	if err := ensureRestoreSpace(dir, 0); err != nil {
		t.Errorf("ensureRestoreSpace with 0 bytes = %v, want nil", err)
	}

	// An absurdly large snapshot can't fit — must fail early with guidance.
	err := ensureRestoreSpace(dir, 1<<60)
	if err == nil {
		t.Fatal("ensureRestoreSpace with a 1EiB snapshot = nil, want insufficient-space error")
	}
	if !strings.Contains(err.Error(), "resize the data volume") {
		t.Errorf("error did not give actionable guidance: %v", err)
	}

	// When free space cannot be verified at all, the check must FAIL (not
	// silently proceed), so the documented pre-flight guarantee holds.
	if err := ensureRestoreSpace(filepath.Join(dir, "does-not-exist"), 1); err == nil {
		t.Error("ensureRestoreSpace on an unstattable path = nil, want error (must not proceed blindly)")
	}

	// A negative size (e.g. a bogus HeadObject ContentLength) must be rejected,
	// not wrapped into a huge uint64 that spuriously passes.
	if err := ensureRestoreSpace(dir, -1); err == nil {
		t.Error("ensureRestoreSpace with a negative size = nil, want error")
	}
}

// writeFakeEtcdutl writes an executable stub standing in for the etcd image's
// etcdutl: it records its argv to argsFile and, mimicking a real restore,
// creates member/ under the dir passed to --data-dir. Lets the exec path be
// exercised without the etcd binary.
func writeFakeEtcdutl(t *testing.T, argsFile string) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		": > \"" + argsFile + "\"\n" +
		"out=\"\"\nprev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  printf '%s\\n' \"$a\" >> \"" + argsFile + "\"\n" +
		"  if [ \"$prev\" = \"--data-dir\" ]; then out=\"$a\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"mkdir -p \"$out/member\"\n" +
		"printf restored > \"$out/member/db\"\n"
	path := filepath.Join(t.TempDir(), "etcdutl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake etcdutl: %v", err)
	}
	return path
}

// RunRestore must exec the (version-matched) etcdutl binary to rebuild the data
// dir, pass the member identity plus --skip-hash-check, and move the result into
// place. The etcdutl is faked so the exec path runs without the etcd image.
func TestRunRestore_ExecsEtcdutlAndMovesIntoPlace(t *testing.T) {
	dataDir := t.TempDir() // empty: no member/ dir, so the no-op gate is passed
	mount := t.TempDir()
	if err := os.WriteFile(filepath.Join(mount, "snap.db"), []byte("snapshot bytes"), 0o644); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")

	t.Setenv(envDataDir, dataDir)
	t.Setenv(envMemberName, "c1-0")
	t.Setenv(envInitialCluster, "c1-0=http://c1-0:2380")
	t.Setenv(envInitialToken, "tok-xyz")
	t.Setenv(envPeerURLs, "http://c1-0:2380")
	t.Setenv(envEtcdutlPath, writeFakeEtcdutl(t, argsFile))
	t.Setenv(envDestKind, "pvc")
	t.Setenv(envPVCMountPath, mount)
	t.Setenv(envPVCSubPath, "snap.db")

	if err := RunRestore(context.Background()); err != nil {
		t.Fatalf("RunRestore = %v, want nil", err)
	}

	// The restored member/ must be moved into the real data dir, and the staging
	// dir cleaned up.
	if _, err := os.Stat(filepath.Join(dataDir, "member", "db")); err != nil {
		t.Errorf("restored member/ not in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".restore")); !os.IsNotExist(err) {
		t.Errorf(".restore staging dir not cleaned up (stat err=%v)", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	for _, want := range []string{"snapshot", "restore", "--skip-hash-check", "--name", "c1-0", "--initial-cluster-token", "tok-xyz"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("etcdutl not invoked with %q; got:\n%s", want, args)
		}
	}
}

// RunInstallTools copies the running binary to TOOLS_DEST_DIR/manager so the
// restore container (etcd image) can exec it.
func TestRunInstallTools(t *testing.T) {
	dest := t.TempDir()
	t.Setenv(envToolsDir, dest)

	if err := RunInstallTools(); err != nil {
		t.Fatalf("RunInstallTools = %v, want nil", err)
	}

	out := filepath.Join(dest, "manager")
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat copied binary: %v", err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("copied binary is not executable: mode %v", fi.Mode())
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	want, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read source binary: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read copied binary: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("copied binary size = %d, want %d (source)", len(got), len(want))
	}
}

// RunInstallTools must fail loudly when its destination is unset rather than
// silently no-op, which would leave the restore container with no binary to exec.
func TestRunInstallTools_NoDestFails(t *testing.T) {
	t.Setenv(envToolsDir, "")
	if err := RunInstallTools(); err == nil {
		t.Error("RunInstallTools with no TOOLS_DEST_DIR = nil, want error")
	}
}

// A crashed prior restore can leave a staged S3 download (etcd-restore-*.db)
// and the etcdutl staging dir (.restore) behind. cleanStaleRestoreArtifacts
// must remove both (so they don't accumulate and don't count against the
// free-space pre-flight) while leaving unrelated files untouched.
func TestCleanStaleRestoreArtifacts(t *testing.T) {
	dataDir := t.TempDir()

	stale := filepath.Join(dataDir, "etcd-restore-123.db")
	if err := os.WriteFile(stale, []byte("partial download"), 0o644); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(dataDir, ".restore", "member")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dataDir, "unrelated.db")
	if err := os.WriteFile(keep, []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cleanStaleRestoreArtifacts(dataDir); err != nil {
		t.Fatalf("cleanStaleRestoreArtifacts: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale download temp not removed (stat err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".restore")); !os.IsNotExist(err) {
		t.Error(".restore staging dir not removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("an unrelated file was removed: %v", err)
	}
}

// fetchS3Snapshot must run the free-space pre-flight BEFORE downloading: a data
// volume too small for the snapshot fails on the check and the download is
// never attempted. This pins the runbook's "fails early" guarantee for S3
// (which previously could only fail with an ENOSPC mid-download).
func TestFetchS3Snapshot_SpaceCheckedBeforeDownload(t *testing.T) {
	dir := t.TempDir()

	t.Run("insufficient space: download not attempted", func(t *testing.T) {
		downloaded := false
		_, err := fetchS3Snapshot(dir,
			func() (int64, error) { return 1 << 60, nil }, // HEAD reports 1EiB
			func() (string, error) { downloaded = true; return "", nil },
		)
		if err == nil {
			t.Fatal("fetchS3Snapshot with an undersized volume = nil, want pre-flight error")
		}
		if !strings.Contains(err.Error(), "resize the data volume") {
			t.Errorf("error did not give the actionable pre-flight guidance: %v", err)
		}
		if downloaded {
			t.Error("download was attempted despite the free-space pre-flight failing")
		}
	})

	t.Run("sufficient space: download proceeds", func(t *testing.T) {
		downloaded := false
		path, err := fetchS3Snapshot(dir,
			func() (int64, error) { return 0, nil }, // tiny snapshot always fits
			func() (string, error) { downloaded = true; return "/data/snap.db", nil },
		)
		if err != nil {
			t.Fatalf("fetchS3Snapshot with ample space = %v, want nil", err)
		}
		if !downloaded {
			t.Error("download was not attempted after the space check passed")
		}
		if path != "/data/snap.db" {
			t.Errorf("path = %q, want the download's result", path)
		}
	})

	t.Run("HEAD failure surfaces before any download", func(t *testing.T) {
		downloaded := false
		_, err := fetchS3Snapshot(dir,
			func() (int64, error) { return 0, errors.New("boom") },
			func() (string, error) { downloaded = true; return "", nil },
		)
		if err == nil || !strings.Contains(err.Error(), "head snapshot in s3") {
			t.Errorf("HEAD failure not surfaced: %v", err)
		}
		if downloaded {
			t.Error("download was attempted despite the HEAD probe failing")
		}
	})

	// A download honoring its (bounded) context must surface the deadline error
	// rather than hang — the restore agent runs RunRestore under a context with
	// RestoreTimeout, threaded into the S3 calls, so a black-holed endpoint can't
	// wedge bootstrap forever.
	t.Run("download deadline surfaces, no hang", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := fetchS3Snapshot(dir,
			func() (int64, error) { return 0, nil }, // head ok, fits
			func() (string, error) { <-ctx.Done(); return "", ctx.Err() },
		)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("want a context.DeadlineExceeded download error, got %v", err)
		}
	})
}

// Even with a non-empty subPath, pointing at a directory must be rejected
// before reaching etcdutl.
func TestRunRestore_PVCDirectorySourceFails(t *testing.T) {
	dataDir := t.TempDir()
	mount := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mount, "subdir"), 0o755); err != nil {
		t.Fatalf("seed subdir: %v", err)
	}

	t.Setenv(envDataDir, dataDir)
	t.Setenv(envDestKind, "pvc")
	t.Setenv(envPVCMountPath, mount)
	t.Setenv(envPVCSubPath, "subdir") // exists, but is a directory

	err := RunRestore(context.Background())
	if err == nil {
		t.Fatal("RunRestore pointing at a directory = nil, want error")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error did not mention the directory problem: %v", err)
	}
}
