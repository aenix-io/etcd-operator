/*
Copyright 2023 Timofey Larkin.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	lll "github.com/cozystack/etcd-operator/api/v1alpha2"
)

func seedMember(restore *lll.RestoreSpec) *lll.EtcdMember {
	return &lll.EtcdMember{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-0", Namespace: "ns", Labels: memberLabels("c1", "c1-0")},
		Spec: lll.EtcdMemberSpec{
			ClusterName:    "c1",
			Version:        "3.6.4",
			InitialCluster: "c1-0=http://c1-0.c1.ns.svc:2380",
			ClusterToken:   "token-xyz",
			Restore:        restore,
		},
	}
}

func findInitContainer(pod *corev1.Pod, name string) (corev1.Container, bool) {
	for _, ic := range pod.Spec.InitContainers {
		if ic.Name == name {
			return ic, true
		}
	}
	return corev1.Container{}, false
}

func initContainerNames(pod *corev1.Pod) []string {
	names := make([]string, len(pod.Spec.InitContainers))
	for i, ic := range pod.Spec.InitContainers {
		names[i] = ic.Name
	}
	return names
}

func TestBuildPod_NoRestoreInitContainerWithoutSpec(t *testing.T) {
	r := &EtcdMemberReconciler{Scheme: testScheme(t), OperatorImage: "operator:latest"}
	pod := r.buildPod(seedMember(nil), false)
	if _, ok := findInitContainer(pod, "restore"); ok {
		t.Error("restore initContainer present though no restore spec was set")
	}
	if _, ok := findInitContainer(pod, "install-tools"); ok {
		t.Error("install-tools initContainer present though no restore spec was set")
	}
}

func TestBuildPod_RestoreInitContainerS3(t *testing.T) {
	restore := &lll.RestoreSpec{Source: lll.SnapshotLocation{
		S3: &lll.S3SnapshotLocation{
			Endpoint:             "https://minio.svc:9000",
			Bucket:               "etcd",
			Key:                  "snapshots/b1.db",
			ForcePathStyle:       true,
			CredentialsSecretRef: corev1.LocalObjectReference{Name: "s3-creds"},
		},
	}}
	r := &EtcdMemberReconciler{Scheme: testScheme(t), OperatorImage: "operator:latest"}
	pod := r.buildPod(seedMember(restore), false)

	// install-tools stages the operator binary onto the shared volume so the
	// restore container (etcd image) can exec it.
	it, ok := findInitContainer(pod, "install-tools")
	if !ok {
		t.Fatal("install-tools initContainer missing")
	}
	if it.Image != "operator:latest" {
		t.Errorf("install-tools image = %q, want operator:latest", it.Image)
	}
	if got, want := it.Command, []string{"/manager", "install-tools"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("install-tools command = %v, want %v", got, want)
	}
	if m, ok := mountByName(it.VolumeMounts, "restore-tools"); !ok || m.MountPath != "/tools" || m.ReadOnly {
		t.Errorf("install-tools restore-tools mount = %+v, want writable at /tools", m)
	}

	// install-tools must precede restore: it stages the binary the restore
	// container execs, so the order is correctness-critical.
	if got := initContainerNames(pod); len(got) != 2 || got[0] != "install-tools" || got[1] != "restore" {
		t.Errorf("initContainer order = %v, want [install-tools restore]", got)
	}

	ic, ok := findInitContainer(pod, "restore")
	if !ok {
		t.Fatal("restore initContainer missing")
	}
	// The restore container runs the target etcd image, so its bundled etcdutl
	// matches spec.version — the whole point of restoring per-version.
	if ic.Image != "quay.io/coreos/etcd:v3.6.4" {
		t.Errorf("restore image = %q, want quay.io/coreos/etcd:v3.6.4 (version-matched)", ic.Image)
	}
	// It execs the operator binary staged on the shared volume, not the etcd image's entrypoint.
	if got, want := ic.Command, []string{"/tools/manager", "restore-agent"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("restore command = %v, want %v", got, want)
	}
	if m, ok := mountByName(ic.VolumeMounts, "restore-tools"); !ok || m.MountPath != "/tools" {
		t.Errorf("restore restore-tools mount = %+v, want /tools", m)
	}

	// Restore identity must match what the etcd container will run with.
	vals, secretKeys := envMap(ic.Env)
	if vals["ETCD_MEMBER_NAME"] != "c1-0" {
		t.Errorf("ETCD_MEMBER_NAME = %q, want c1-0", vals["ETCD_MEMBER_NAME"])
	}
	if vals["ETCD_INITIAL_CLUSTER"] != "c1-0=http://c1-0.c1.ns.svc:2380" {
		t.Errorf("ETCD_INITIAL_CLUSTER = %q", vals["ETCD_INITIAL_CLUSTER"])
	}
	if vals["ETCD_INITIAL_CLUSTER_TOKEN"] != "token-xyz" {
		t.Errorf("ETCD_INITIAL_CLUSTER_TOKEN = %q, want token-xyz", vals["ETCD_INITIAL_CLUSTER_TOKEN"])
	}
	if vals["ETCD_DATA_DIR"] != "/var/lib/etcd" {
		t.Errorf("ETCD_DATA_DIR = %q, want /var/lib/etcd", vals["ETCD_DATA_DIR"])
	}
	if vals["SNAPSHOT_DEST_KIND"] != "s3" || vals["S3_KEY"] != "snapshots/b1.db" {
		t.Errorf("s3 source env = %+v", vals)
	}
	if got, ok := secretKeys["AWS_ACCESS_KEY_ID"]; !ok || got[0] != "s3-creds" {
		t.Errorf("AWS_ACCESS_KEY_ID secret ref = %v", got)
	}

	// The initContainer must share the etcd data volume so the restored data
	// dir is visible to the etcd container.
	if m, ok := mountByName(ic.VolumeMounts, "data"); !ok || m.MountPath != "/var/lib/etcd" {
		t.Errorf("data mount = %+v, want /var/lib/etcd", m)
	}

	// Requests must be set: a BestEffort restore init container gating
	// bootstrap is an eviction risk.
	if ic.Resources.Requests.Cpu().IsZero() || ic.Resources.Requests.Memory().IsZero() {
		t.Errorf("restore init container has no resource requests: %+v", ic.Resources.Requests)
	}
	// But it must carry NO memory limit: etcdutl snapshot.Restore's working set
	// scales with the snapshot/keyspace, so a fixed low ceiling would OOM-kill a
	// large restore and brick bootstrap. The rebuild must be free to use node
	// memory.
	if _, ok := ic.Resources.Limits[corev1.ResourceMemory]; ok {
		t.Errorf("restore init container has a memory limit (%v); a large restore would OOM-kill and brick bootstrap",
			ic.Resources.Limits.Memory())
	}
}

// etcd and the restore agent never call the kube API; the seed Pod must not
// auto-mount a ServiceAccount token.
func TestBuildPod_NoServiceAccountToken(t *testing.T) {
	r := &EtcdMemberReconciler{Scheme: testScheme(t), OperatorImage: "operator:latest"}
	pod := r.buildPod(seedMember(nil), false)
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Errorf("AutomountServiceAccountToken = %v, want explicit false", pod.Spec.AutomountServiceAccountToken)
	}
}

// A seed that requests a restore must NOT get a Pod with an empty restore
// image — that would brick bootstrap. ensurePod must error and create nothing.
func TestEnsurePod_RestoreWithoutOperatorImageRefuses(t *testing.T) {
	member := seedMember(&lll.RestoreSpec{Source: lll.SnapshotLocation{
		PVC: &lll.PVCSnapshotLocation{ClaimName: "snap-pvc", SubPath: "b1.db"},
	}})
	c, s := newTestClient(t, member)
	r := &EtcdMemberReconciler{Client: c, Scheme: s, OperatorImage: ""}

	err := r.ensurePod(context.Background(), member)
	if err == nil {
		t.Fatal("ensurePod created a Pod for a restore seed with no operator image; want error")
	}
	if !strings.Contains(err.Error(), "operator image is not configured") {
		t.Errorf("error did not mention missing operator image: %v", err)
	}
	// No Pod must exist.
	if getErr := c.Get(context.Background(), types.NamespacedName{Name: member.Name, Namespace: member.Namespace}, &corev1.Pod{}); !apierrors.IsNotFound(getErr) {
		t.Errorf("expected no Pod created, got err=%v", getErr)
	}
}

func TestBuildPod_RestoreInitContainerPVC(t *testing.T) {
	restore := &lll.RestoreSpec{Source: lll.SnapshotLocation{
		PVC: &lll.PVCSnapshotLocation{ClaimName: "snap-pvc", SubPath: "b1.db"},
	}}
	r := &EtcdMemberReconciler{Scheme: testScheme(t), OperatorImage: "operator:latest"}
	pod := r.buildPod(seedMember(restore), false)

	ic, ok := findInitContainer(pod, "restore")
	if !ok {
		t.Fatal("restore initContainer missing")
	}
	vals, _ := envMap(ic.Env)
	if vals["SNAPSHOT_DEST_KIND"] != "pvc" || vals["PVC_SUBPATH"] != "b1.db" {
		t.Errorf("pvc source env = %+v", vals)
	}

	// Source PVC mounted read-only.
	v, ok := volumeByName(pod.Spec.Volumes, "restore-src")
	if !ok || v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "snap-pvc" || !v.PersistentVolumeClaim.ReadOnly {
		t.Errorf("restore-src volume = %+v, want read-only claim snap-pvc", v)
	}
	if m, ok := mountByName(ic.VolumeMounts, "restore-src"); !ok || !m.ReadOnly {
		t.Errorf("restore-src mount = %+v, want read-only", m)
	}
}
