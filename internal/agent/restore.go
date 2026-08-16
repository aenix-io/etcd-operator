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
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// RunRestore populates the etcd data dir from a snapshot before etcd starts.
// It runs as an initContainer on the bootstrap seed Pod. It is idempotent: if
// the data dir is already initialized (a `member/` directory exists), it is a
// no-op, so Pod restarts after the first boot leave the live data untouched.
//
// For a restore SOURCE the destination locators are EXACT (not prefixes):
// S3_KEY is the full object key, PVC_SUBPATH the full file path within the
// mounted source volume.
func RunRestore(ctx context.Context) error {
	dataDir := os.Getenv(envDataDir)
	if dataDir == "" {
		dataDir = "/var/lib/etcd"
	}
	memberDir := filepath.Join(dataDir, "member")
	if _, err := os.Stat(memberDir); err == nil {
		fmt.Printf("restore: %s already initialized, skipping\n", memberDir)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", memberDir, err)
	}

	src, err := loadDestination()
	if err != nil {
		return err
	}

	// For a restore SOURCE the S3 key is the EXACT object (not a prefix). An
	// empty key would issue a GetObject for "" and fail with an opaque S3
	// error inside the seed init container — bricking bootstrap. Catch it
	// early with a clear message. (CEL also rejects this at the apiserver, but
	// the agent must not depend on that being enforced.)
	if src.kind == "s3" && src.s3Key == "" {
		return fmt.Errorf("restore source requires an exact S3 object key (%s); got empty", envS3Key)
	}
	// Symmetric to the S3 guard: a PVC restore source addresses one exact file
	// within the volume. An empty subPath would resolve to the mount directory,
	// which os.Stat happily accepts — etcdutl would then fail opaquely trying to
	// read a directory as a snapshot, bricking bootstrap. Require it explicitly.
	if src.kind == "pvc" && src.pvcSubPath == "" {
		return fmt.Errorf("restore source requires the exact snapshot file path within the volume (%s); got empty", envPVCSubPath)
	}

	// A prior attempt may have crashed (OOM, node reboot) after staging a
	// snapshot download but before its deferred cleanup ran. We are past the
	// member/ no-op gate, so the data dir is uninitialized and any staged
	// artifacts are stale debris — remove them before the free-space pre-flight,
	// which would otherwise count leftover downloads against the headroom it is
	// trying to protect (and they would accumulate across retries).
	if err := cleanStaleRestoreArtifacts(dataDir); err != nil {
		return err
	}

	// Obtain the snapshot file. The free-space pre-flight runs BEFORE the
	// expensive step in each branch (per the operations runbook's "fails early"
	// guarantee): etcdutl rebuilds the data dir (~snapshot-sized) and, for S3,
	// the download stages onto the same data volume first — a transient ~2x
	// footprint. If we cannot determine the size or free space we fail rather
	// than proceed blindly.
	var snapPath string
	switch src.kind {
	case "s3":
		// HeadObject gives the snapshot size without transferring it, so a data
		// volume too small to even hold the download fails on the pre-flight
		// with a clear, actionable message rather than as an opaque ENOSPC
		// partway through the download itself (the download stages INTO the
		// data dir, not the container's ephemeral /tmp).
		snapPath, err = fetchS3Snapshot(dataDir,
			func() (int64, error) { return headSnapshotSizeS3(ctx, src) },
			func() (string, error) { return downloadS3(ctx, src, dataDir) })
		if err != nil {
			return err
		}
		defer os.Remove(snapPath)
	case "pvc":
		// The snapshot lives on the read-only source mount, not the data
		// volume, so only the etcdutl rebuild consumes data-dir space — the ~2x
		// headroom check is conservative here, which is safe.
		snapPath = filepath.Join(src.pvcMount, src.pvcSubPath)
		fi, err := os.Stat(snapPath)
		if err != nil {
			return fmt.Errorf("snapshot file %s: %w", snapPath, err)
		}
		if fi.IsDir() {
			return fmt.Errorf("snapshot source %s is a directory, not a snapshot file", snapPath)
		}
		if err := ensureRestoreSpace(dataDir, fi.Size()); err != nil {
			return err
		}
	}

	// etcdutl refuses a non-empty output dir, so restore into a fresh staging
	// subdir, then move member/ into the real data dir so etcd's --data-dir stays
	// /var/lib/etcd.
	staging := filepath.Join(dataDir, ".restore")
	_ = os.RemoveAll(staging) // clean any partial prior attempt

	if err := runEtcdutlRestore(ctx, snapPath, staging); err != nil {
		return err
	}

	if err := os.Rename(filepath.Join(staging, "member"), memberDir); err != nil {
		return fmt.Errorf("move restored data into place: %w", err)
	}
	_ = os.RemoveAll(staging)

	fmt.Printf("restore: completed into %s\n", memberDir)
	return nil
}

// runEtcdutlRestore rebuilds the data dir under outputDir from snapPath by
// exec-ing the etcdutl binary shipped in the target etcd image — the version
// this cluster runs — rather than a single compiled-in one, so restore works
// across etcd minors. --skip-hash-check is required: a clientv3
// Maintenance.Snapshot stream (how the snapshot agent captures snapshots) has
// no appended integrity hash, unlike `etcdutl snapshot save`.
func runEtcdutlRestore(ctx context.Context, snapPath, outputDir string) error {
	etcdutl := os.Getenv(envEtcdutlPath)
	if etcdutl == "" {
		etcdutl = defaultEtcdutlPath
	}

	args := []string{
		"snapshot", "restore", snapPath,
		"--data-dir", outputDir,
		"--name", os.Getenv(envMemberName),
		"--initial-cluster", os.Getenv(envInitialCluster),
		"--initial-cluster-token", os.Getenv(envInitialToken),
		"--skip-hash-check",
	}
	if p := os.Getenv(envPeerURLs); p != "" {
		args = append(args, "--initial-advertise-peer-urls", p)
	}

	cmd := exec.CommandContext(ctx, etcdutl, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("etcdutl snapshot restore: %w", err)
	}
	return nil
}

// RunInstallTools copies the running operator binary into envToolsDir so the
// restore initContainer — which runs from the target etcd image, not the
// operator image — can exec it while still reaching that image's version-matched
// etcdutl. This bridges two distroless images that share no binaries: the etcd
// image has etcdutl but no way to copy it out, so we bring the operator to it.
func RunInstallTools() error {
	dest := os.Getenv(envToolsDir)
	if dest == "" {
		return fmt.Errorf("%s is not set; nowhere to install the operator binary", envToolsDir)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create tools dir %s: %w", dest, err)
	}
	out := filepath.Join(dest, "manager")
	if err := copyExecutable(self, out); err != nil {
		return err
	}
	fmt.Printf("install-tools: copied %s to %s\n", self, out)
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("finalize %s: %w", dst, err)
	}
	return nil
}

// cleanStaleRestoreArtifacts removes leftover staging from a crashed prior
// restore attempt: the S3 download temp files (etcd-restore-*.db, matching
// downloadS3's os.CreateTemp pattern) and the etcdutl staging dir (.restore),
// both staged in the data dir. Only called when the data dir is uninitialized
// (past the member/ no-op gate), so nothing live is at risk.
func cleanStaleRestoreArtifacts(dataDir string) error {
	matches, err := filepath.Glob(filepath.Join(dataDir, "etcd-restore-*.db"))
	if err != nil {
		return fmt.Errorf("scan for stale restore artifacts: %w", err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale restore artifact %s: %w", m, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(dataDir, ".restore")); err != nil {
		return fmt.Errorf("remove stale restore staging dir: %w", err)
	}
	return nil
}

// fetchS3Snapshot enforces the head-before-download ordering that makes the
// "fails early on a too-small volume" guarantee real: it probes the snapshot
// size (headSize), runs the free-space pre-flight, and only invokes download if
// that passes — so an undersized data volume never even starts the transfer.
// head and download are injected so the ordering is unit-testable without S3.
func fetchS3Snapshot(dataDir string, headSize func() (int64, error), download func() (string, error)) (string, error) {
	size, err := headSize()
	if err != nil {
		return "", fmt.Errorf("head snapshot in s3: %w", err)
	}
	if err := ensureRestoreSpace(dataDir, size); err != nil {
		return "", err
	}
	path, err := download()
	if err != nil {
		return "", fmt.Errorf("download snapshot from s3: %w", err)
	}
	return path, nil
}

// headSnapshotSizeS3 returns the snapshot object's size via HeadObject, without
// downloading it — so the restore free-space pre-flight can run before the
// download consumes any of the data volume.
func headSnapshotSizeS3(ctx context.Context, src destination) (int64, error) {
	client, err := src.s3Client(ctx)
	if err != nil {
		return 0, err
	}
	out, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(src.s3Bucket),
		Key:    aws.String(src.s3Key), // exact object key for restore
	})
	if err != nil {
		return 0, err
	}
	if out.ContentLength == nil {
		return 0, fmt.Errorf("s3 HeadObject returned no ContentLength for %s", src.s3Key)
	}
	return *out.ContentLength, nil
}

// downloadS3 fetches the snapshot into stageDir (the data volume), returning
// the local path. The caller removes it after the restore.
func downloadS3(ctx context.Context, src destination, stageDir string) (string, error) {
	client, err := src.s3Client(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(stageDir, "etcd-restore-*.db")
	if err != nil {
		return "", err
	}
	defer f.Close()
	downloader := manager.NewDownloader(client)
	if _, err := downloader.Download(ctx, f, &s3.GetObjectInput{
		Bucket: aws.String(src.s3Bucket),
		Key:    aws.String(src.s3Key), // exact object key for restore
	}); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// restoreStagingFactor is the rough multiple of the snapshot size the data
// volume must have free during a restore: the snapshot we already hold plus
// the data dir etcdutl rebuilds from it.
const restoreStagingFactor = 2

// ensureRestoreSpace fails early (with an actionable message) when the data
// volume lacks headroom to stage a restore of a snapSize-byte snapshot, rather
// than letting etcdutl die with an opaque ENOSPC partway through.
func ensureRestoreSpace(dataDir string, snapSize int64) error {
	if snapSize < 0 {
		// A negative size (e.g. a bogus HeadObject ContentLength) would wrap to a
		// huge uint64 below and spuriously pass the check — reject it instead.
		return fmt.Errorf("snapshot reports a negative size (%d bytes); refusing to restore", snapSize)
	}
	avail, err := availableBytes(dataDir)
	if err != nil {
		// Can't verify free space — fail rather than proceed blindly, so the
		// documented pre-flight guarantee actually holds.
		return fmt.Errorf("check free space on %s: %w", dataDir, err)
	}
	need := uint64(snapSize) * restoreStagingFactor
	if avail < need {
		return fmt.Errorf("data dir %s has %d bytes free but restoring a %d-byte snapshot needs ~%d (≈%dx for staging); resize the data volume",
			dataDir, avail, snapSize, need, restoreStagingFactor)
	}
	return nil
}

// availableBytes returns the bytes available to an unprivileged writer on the
// filesystem backing dir.
func availableBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
