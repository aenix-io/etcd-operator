/*
Copyright 2023 Timofey Larkin.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// fakeUploader drains the upload Body exactly as the real manager.Uploader
// does, so a test sees the full streamed payload.
type fakeUploader struct {
	got []byte
	err error
}

func (f *fakeUploader) Upload(_ context.Context, in *s3.PutObjectInput, _ ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	b, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.got = b
	return &manager.UploadOutput{}, nil
}

// uploadStreamHashed must stream the body straight through (no local staging),
// returning the exact byte count and sha256 of what the uploader stored.
func TestUploadStreamHashed(t *testing.T) {
	const payload = "fake etcd snapshot stream bytes"
	up := &fakeUploader{}

	size, sum, err := uploadStreamHashed(context.Background(), up,
		&s3.PutObjectInput{Bucket: aws.String("etcd"), Key: aws.String("snap.db")},
		strings.NewReader(payload))
	if err != nil {
		t.Fatalf("uploadStreamHashed: %v", err)
	}
	if size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", size, len(payload))
	}
	h := sha256.Sum256([]byte(payload))
	if sum != hex.EncodeToString(h[:]) {
		t.Errorf("sha256 = %q, want %q", sum, hex.EncodeToString(h[:]))
	}
	if string(up.got) != payload {
		t.Errorf("uploader received %q, want the full stream %q", up.got, payload)
	}
}

func TestUploadStreamHashed_UploadError(t *testing.T) {
	_, _, err := uploadStreamHashed(context.Background(), &fakeUploader{err: errors.New("s3 down")},
		&s3.PutObjectInput{Bucket: aws.String("etcd"), Key: aws.String("snap.db")},
		strings.NewReader("x"))
	if err == nil {
		t.Fatal("uploadStreamHashed with a failing uploader = nil, want error")
	}
}

// capturedReq records the checksum-related surface of one request the fake S3
// endpoint received, so a test can assert no flexible-checksum trailer was sent.
type capturedReq struct{ method, sha, trailer, algo, partNumber string }

// startS3CaptureServer stands up a TLS httptest server that speaks just enough of
// the S3 protocol for uploadS3Stream to run against — HeadObject, single-part
// PutObject, and the multipart initiate/part/complete trio — trusts its cert
// through the SDK default config chain (AWS_CA_BUNDLE, which LoadDefaultConfig
// wires into the real client, so s3Client and newSnapshotUploader run exactly as
// in production), and records every request's checksum headers. The returned func
// snapshots the captured requests.
func startS3CaptureServer(t *testing.T) (endpoint string, captured func() []capturedReq) {
	t.Helper()

	var mu sync.Mutex
	var reqs []capturedReq

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		q := r.URL.Query()
		mu.Lock()
		reqs = append(reqs, capturedReq{
			method:     r.Method,
			sha:        r.Header.Get("X-Amz-Content-Sha256"),
			trailer:    r.Header.Get("X-Amz-Trailer"),
			algo:       r.Header.Get("X-Amz-Sdk-Checksum-Algorithm"),
			partNumber: q.Get("partNumber"),
		})
		mu.Unlock()

		switch {
		case r.Method == http.MethodHead:
			// ensureObjectAbsent's HeadObject: report absent so the upload proceeds.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key>`+
				`<UploadId>test-upload-id</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			w.Header().Set("ETag", `"etag-`+q.Get("partNumber")+`"`)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && q.Get("uploadId") != "":
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<CompleteMultipartUploadResult><Bucket>b</Bucket><Key>k</Key>`+
				`<ETag>"final-etag"</ETag></CompleteMultipartUploadResult>`)
		case r.Method == http.MethodPut:
			// single-part PutObject (body under the manager's 5 MiB part size)
			w.Header().Set("ETag", `"single-etag"`)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}
	t.Setenv("AWS_CA_BUNDLE", caFile)

	return srv.URL, func() []capturedReq {
		mu.Lock()
		defer mu.Unlock()
		out := make([]capturedReq, len(reqs))
		copy(out, reqs)
		return out
	}
}

// assertNoChecksumTrailer fails if a request carries any of the flexible-checksum
// surface that Ceph RGW rejects: the streaming-trailer content-sha256, an
// x-amz-trailer, or an x-amz-sdk-checksum-algorithm.
func assertNoChecksumTrailer(t *testing.T, rq capturedReq) {
	t.Helper()
	if rq.sha == "STREAMING-UNSIGNED-PAYLOAD-TRAILER" {
		t.Errorf("%s carried x-amz-content-sha256=STREAMING-UNSIGNED-PAYLOAD-TRAILER — the checksum trailer Ceph RGW rejects", rq.method)
	}
	if rq.trailer != "" {
		t.Errorf("%s carried x-amz-trailer=%q, want none", rq.method, rq.trailer)
	}
	if rq.algo != "" {
		t.Errorf("%s carried x-amz-sdk-checksum-algorithm=%q, want none", rq.method, rq.algo)
	}
}

// TestUploadS3StreamMultipartNoChecksumTrailer is the guard for the actual
// failure mode. A snapshot of a non-trivial cluster exceeds the transfer manager's
// 5 MiB part size, so the upload takes the MULTIPART branch, where manager.Uploader's
// own RequestChecksumCalculation (NOT the s3.Client option) decides whether a CRC32
// trailer is stamped on each UploadPart. Without newSnapshotUploader pinning it
// to WhenRequired, every part rides `x-amz-content-sha256:
// STREAMING-UNSIGNED-PAYLOAD-TRAILER` + `x-amz-trailer: x-amz-checksum-crc32` —
// exactly the header Ceph RGW rejects.
//
// It fails if the uploader option is dropped; a check on s3.Options alone (see
// agent_test.go) would stay green while this real path stayed broken.
func TestUploadS3StreamMultipartNoChecksumTrailer(t *testing.T) {
	hermeticAWSEnv(t)
	endpoint, captured := startS3CaptureServer(t)

	const bodySize = 12 << 20 // 12 MiB > 5 MiB part size ⇒ three parts ⇒ multipart branch
	dest := destination{kind: "s3", s3Endpoint: endpoint, s3Bucket: "b", s3PathStyle: true}

	size, sum, err := uploadS3Stream(context.Background(), dest, "k", bytes.NewReader(make([]byte, bodySize)), "test-uid")
	if err != nil {
		t.Fatalf("uploadS3Stream: %v", err)
	}
	if size != bodySize {
		t.Errorf("streamed size = %d, want %d", size, bodySize)
	}
	if sum == "" {
		t.Error("empty sha256 digest")
	}

	var sawUploadPart bool
	for _, rq := range captured() {
		if rq.method == http.MethodPut && rq.partNumber != "" {
			sawUploadPart = true
		}
		assertNoChecksumTrailer(t, rq)
	}
	if !sawUploadPart {
		t.Fatal("no UploadPart (PUT with partNumber) seen — the upload did not take the multipart branch, so this test is not exercising the path it guards")
	}
}

// TestUploadS3StreamSinglePartNoChecksumTrailer guards the OTHER upload branch.
// A snapshot of a small or freshly bootstrapped cluster is well under the transfer
// manager's 5 MiB part size, so the manager issues a single-part PutObject rather
// than multipart. There the s3.Client option (not the uploader's) is the only
// thing keeping the CRC32 trailer off — so this pins the client-side setting
// end-to-end. A struct-field check on s3.Options (agent_test.go) stays green even
// when the real request carries the trailer — that gap is how a client-only pin
// can look correct while every upload is still broken on the wire.
func TestUploadS3StreamSinglePartNoChecksumTrailer(t *testing.T) {
	hermeticAWSEnv(t)
	endpoint, captured := startS3CaptureServer(t)

	const bodySize = 1 << 20 // 1 MiB < 5 MiB part size ⇒ single-part PutObject branch
	dest := destination{kind: "s3", s3Endpoint: endpoint, s3Bucket: "b", s3PathStyle: true}

	size, sum, err := uploadS3Stream(context.Background(), dest, "k", bytes.NewReader(make([]byte, bodySize)), "test-uid")
	if err != nil {
		t.Fatalf("uploadS3Stream: %v", err)
	}
	if size != bodySize {
		t.Errorf("streamed size = %d, want %d", size, bodySize)
	}
	if sum == "" {
		t.Error("empty sha256 digest")
	}

	var putObjects int
	for _, rq := range captured() {
		if rq.method == http.MethodPut && rq.partNumber == "" {
			putObjects++
		}
		if rq.partNumber != "" {
			t.Errorf("saw a multipart UploadPart (partNumber=%q); a %d-byte body must take the single-part branch this test guards", rq.partNumber, bodySize)
		}
		assertNoChecksumTrailer(t, rq)
	}
	if putObjects != 1 {
		t.Fatalf("saw %d single-part PutObject PUTs, want exactly 1 — the body no longer takes the single-part branch, so this test is not exercising the path it guards", putObjects)
	}
}

type fakeHead struct {
	out *s3.HeadObjectOutput
	err error
}

func (f fakeHead) HeadObject(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return f.out, f.err
}

func TestEnsureObjectAbsent(t *testing.T) {
	ctx := context.Background()

	t.Run("foreign object refused", func(t *testing.T) {
		// Exists with no/other ownership stamp → refuse.
		err := ensureObjectAbsent(ctx, fakeHead{out: &s3.HeadObjectOutput{}}, "etcd", "snap.db", "uid-1")
		if err == nil {
			t.Fatal("ensureObjectAbsent on a foreign object = nil, want refuse error")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("error did not mention overwrite refusal: %v", err)
		}
	})

	t.Run("own object from a prior attempt is ok (idempotent retry)", func(t *testing.T) {
		out := &s3.HeadObjectOutput{Metadata: map[string]string{snapshotUIDMetaKey: "uid-1"}}
		if err := ensureObjectAbsent(ctx, fakeHead{out: out}, "etcd", "snap.db", "uid-1"); err != nil {
			t.Errorf("an object stamped with our own UID must not block a retry: %v", err)
		}
	})

	t.Run("object owned by a different snapshot refused", func(t *testing.T) {
		out := &s3.HeadObjectOutput{Metadata: map[string]string{snapshotUIDMetaKey: "someone-else"}}
		if err := ensureObjectAbsent(ctx, fakeHead{out: out}, "etcd", "snap.db", "uid-1"); err == nil {
			t.Fatal("an object owned by a different snapshot must be refused")
		}
	})

	t.Run("typed NotFound is ok", func(t *testing.T) {
		if err := ensureObjectAbsent(ctx, fakeHead{err: &s3types.NotFound{}}, "etcd", "snap.db", "uid-1"); err != nil {
			t.Errorf("typed NotFound should be treated as absent: %v", err)
		}
	})

	t.Run("smithy 404 code is ok", func(t *testing.T) {
		e := &smithy.GenericAPIError{Code: "NotFound", Message: "Not Found"}
		if err := ensureObjectAbsent(ctx, fakeHead{err: e}, "etcd", "snap.db", "uid-1"); err != nil {
			t.Errorf("smithy NotFound should be treated as absent: %v", err)
		}
	})

	t.Run("other error propagates", func(t *testing.T) {
		err := ensureObjectAbsent(ctx, fakeHead{err: errors.New("network down")}, "etcd", "snap.db", "uid-1")
		if err == nil {
			t.Fatal("a non-NotFound HeadObject error must propagate, not be treated as absent")
		}
		if !strings.Contains(err.Error(), "check for existing") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	// A 403 AccessDenied on HEAD of a missing key (S3/MinIO/Ceph return this
	// when the credentials lack ListBucket) is NOT treated as "absent": we fail
	// closed rather than risk overwriting. The runbook documents that snapshot
	// credentials must allow HEAD-on-missing to return 404 (i.e. ListBucket).
	t.Run("403 access denied fails closed (not treated as absent)", func(t *testing.T) {
		e := &smithy.GenericAPIError{Code: "AccessDenied", Message: "Access Denied"}
		err := ensureObjectAbsent(ctx, fakeHead{err: e}, "etcd", "snap.db", "uid-1")
		if err == nil {
			t.Fatal("a 403 AccessDenied must fail closed, not be treated as absent")
		}
		if !strings.Contains(err.Error(), "check for existing") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestEnsureFileAbsent(t *testing.T) {
	t.Run("absent path is ok", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "snap.db")
		if err := ensureFileAbsent(p, "uid-1"); err != nil {
			t.Errorf("ensureFileAbsent on a missing file = %v, want nil", err)
		}
	})

	t.Run("foreign file (no ownership sidecar) refused", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "snap.db")
		if err := os.WriteFile(p, []byte("someone else's snapshot"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := ensureFileAbsent(p, "uid-1")
		if err == nil {
			t.Fatal("ensureFileAbsent on a foreign file = nil, want refuse error")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("error did not mention overwrite refusal: %v", err)
		}
	})

	t.Run("own file from a prior attempt is ok (idempotent retry)", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "snap.db")
		if err := os.WriteFile(p, []byte("our snapshot"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+snapshotUIDSuffix, []byte("uid-1"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureFileAbsent(p, "uid-1"); err != nil {
			t.Errorf("a file stamped with our own UID must not block a retry: %v", err)
		}
	})

	t.Run("file owned by a different snapshot refused", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "snap.db")
		if err := os.WriteFile(p, []byte("their snapshot"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+snapshotUIDSuffix, []byte("someone-else"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureFileAbsent(p, "uid-1"); err == nil {
			t.Fatal("a file owned by a different snapshot must be refused")
		}
	})
}

// errAfter returns n good bytes then fails — simulates a snapshot stream that
// dies mid-transfer.
type errAfter struct {
	data []byte
	pos  int
}

func (e *errAfter) Read(p []byte) (int, error) {
	if e.pos >= len(e.data) {
		return 0, fmt.Errorf("simulated stream failure")
	}
	n := copy(p, e.data[e.pos:])
	e.pos += n
	return n, nil
}

func TestWriteSnapshot(t *testing.T) {
	const payload = "fake etcd snapshot bytes"
	path := filepath.Join(t.TempDir(), "snap.db")

	size, sum, err := writeSnapshot(path, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("writeSnapshot: %v", err)
	}

	if want := int64(len(payload)); size != want {
		t.Errorf("size = %d, want %d", size, want)
	}

	h := sha256.Sum256([]byte(payload))
	if want := hex.EncodeToString(h[:]); sum != want {
		t.Errorf("sha256 = %q, want %q", sum, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != payload {
		t.Errorf("written bytes = %q, want %q", got, payload)
	}
}

func leftoverTmp(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			return true
		}
	}
	return false
}

func TestWriteSnapshotAtomic_Success(t *testing.T) {
	const payload = "atomic snapshot bytes"
	dir := t.TempDir()
	final := filepath.Join(dir, "sub", "snap.db") // exercises MkdirAll too

	size, sum, err := writeSnapshotAtomic(final, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("writeSnapshotAtomic: %v", err)
	}
	if size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", size, len(payload))
	}
	h := sha256.Sum256([]byte(payload))
	if sum != hex.EncodeToString(h[:]) {
		t.Errorf("sha256 mismatch")
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if string(got) != payload {
		t.Errorf("final content = %q, want %q", got, payload)
	}
	if leftoverTmp(t, filepath.Dir(final)) {
		t.Error("a .tmp staging file was left behind after success")
	}
}

// A stream that dies mid-write must leave NO file at the final path (and no
// staging temp), so a later restore never loads a truncated snapshot.
func TestWriteSnapshotAtomic_FailureLeavesNoFinal(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "snap.db")

	_, _, err := writeSnapshotAtomic(final, &errAfter{data: []byte("partial")})
	if err == nil {
		t.Fatal("writeSnapshotAtomic with failing reader = nil, want error")
	}
	if _, statErr := os.Stat(final); !os.IsNotExist(statErr) {
		t.Errorf("final path exists after a failed write (stat err=%v); want absent", statErr)
	}
	if leftoverTmp(t, dir) {
		t.Error("a .tmp staging file was left behind after failure")
	}
}
