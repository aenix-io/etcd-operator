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
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestObjectKey(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "snap", prefix: "", want: "snap.db"},
		{name: "snap", prefix: "snapshots", want: "snapshots/snap.db"},
		{name: "snap", prefix: "snapshots/", want: "snapshots/snap.db"},
		{name: "snap", prefix: "a/b/c", want: "a/b/c/snap.db"},
	}
	for _, tc := range cases {
		d := destination{kind: "s3", s3Key: tc.prefix}
		if got := d.objectKey(tc.name); got != tc.want {
			t.Errorf("objectKey(prefix=%q, name=%q) = %q, want %q", tc.prefix, tc.name, got, tc.want)
		}
	}
}

func TestS3ClientChecksumWhenRequired(t *testing.T) {
	d := destination{
		kind:        "s3",
		s3Endpoint:  "https://s3.example.internal",
		s3PathStyle: true,
	}
	c, err := d.s3Client(context.Background())
	if err != nil {
		t.Fatalf("s3Client: %v", err)
	}
	o := c.Options()
	// Non-AWS S3-compatible backends (Ceph RGW, some MinIO/R2) reject the
	// aws-sdk-go-v2 default (WhenSupported) flexible checksum on PutObject, so
	// the agent must only add a checksum when the operation requires one.
	if o.RequestChecksumCalculation != aws.RequestChecksumCalculationWhenRequired {
		t.Errorf("RequestChecksumCalculation = %v, want WhenRequired (%v)",
			o.RequestChecksumCalculation, aws.RequestChecksumCalculationWhenRequired)
	}
	if !o.UsePathStyle {
		t.Error("UsePathStyle = false, want true")
	}
	if o.BaseEndpoint == nil || *o.BaseEndpoint != d.s3Endpoint {
		t.Errorf("BaseEndpoint = %v, want %q", o.BaseEndpoint, d.s3Endpoint)
	}
}

func TestURI(t *testing.T) {
	s3 := destination{kind: "s3", s3Bucket: "bucket", s3Key: "pre"}
	if got, want := s3.uri("snap"), "s3://bucket/pre/snap.db"; got != want {
		t.Errorf("s3 uri = %q, want %q", got, want)
	}

	pvc := destination{kind: "pvc", pvcMount: "/snapshot/data", pvcSubPath: "sub"}
	if got, want := pvc.uri("snap"), "file:///snapshot/data/sub/snap.db"; got != want {
		t.Errorf("pvc uri = %q, want %q", got, want)
	}
}

func TestLocalPath(t *testing.T) {
	cases := []struct {
		mount   string
		subPath string
		want    string
	}{
		{mount: "/snapshot/data", subPath: "", want: "/snapshot/data/snap.db"},
		{mount: "/snapshot/data", subPath: "sub", want: "/snapshot/data/sub/snap.db"},
	}
	for _, tc := range cases {
		d := destination{kind: "pvc", pvcMount: tc.mount, pvcSubPath: tc.subPath}
		if got := d.localPath("snap"); got != tc.want {
			t.Errorf("localPath(mount=%q, sub=%q) = %q, want %q", tc.mount, tc.subPath, got, tc.want)
		}
	}
}

func TestLoadDestination(t *testing.T) {
	t.Run("s3 valid", func(t *testing.T) {
		t.Setenv(envDestKind, "s3")
		t.Setenv(envS3Endpoint, "https://s3.example.com")
		t.Setenv(envS3Bucket, "bucket")
		t.Setenv(envS3PathStyle, "true")
		d, err := loadDestination()
		if err != nil {
			t.Fatalf("loadDestination: %v", err)
		}
		if d.kind != "s3" || d.s3Bucket != "bucket" || !d.s3PathStyle {
			t.Fatalf("unexpected destination: %+v", d)
		}
	})

	t.Run("s3 missing bucket", func(t *testing.T) {
		t.Setenv(envDestKind, "s3")
		t.Setenv(envS3Endpoint, "https://s3.example.com")
		t.Setenv(envS3Bucket, "")
		if _, err := loadDestination(); err == nil {
			t.Fatal("expected error for missing bucket")
		}
	})

	t.Run("pvc valid", func(t *testing.T) {
		t.Setenv(envDestKind, "pvc")
		t.Setenv(envPVCMountPath, "/snapshot/data")
		d, err := loadDestination()
		if err != nil {
			t.Fatalf("loadDestination: %v", err)
		}
		if d.kind != "pvc" || d.pvcMount != "/snapshot/data" {
			t.Fatalf("unexpected destination: %+v", d)
		}
	})

	t.Run("pvc missing mount", func(t *testing.T) {
		t.Setenv(envDestKind, "pvc")
		t.Setenv(envPVCMountPath, "")
		if _, err := loadDestination(); err == nil {
			t.Fatal("expected error for missing pvc mount")
		}
	})

	t.Run("unknown kind", func(t *testing.T) {
		t.Setenv(envDestKind, "ftp")
		if _, err := loadDestination(); err == nil {
			t.Fatal("expected error for unknown destination kind")
		}
	})
}
