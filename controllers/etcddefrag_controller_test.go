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
	"errors"
	"fmt"
	"strings"
	"testing"

	etcdserverpb "go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lll "github.com/cozystack/etcd-operator/api/v1alpha2"
)

var gib = int64(1 << 30)

func TestDefragRuleTriggered(t *testing.T) {
	q := func(s string) *resource.Quantity { v := resource.MustParse(s); return &v }
	quota := 2 * gib
	cases := []struct {
		name                string
		rule                *lll.DefragRule
		dbSize, dbSizeInUse int64
		want                bool
	}{
		{"nil rule, no fragmentation", nil, 100 << 20, 100 << 20, false},
		{"nil rule, fragmentation over 200Mi default", nil, 500 << 20, 100 << 20, true},
		{"all: unconditional even with nothing to reclaim", &lll.DefragRule{All: true}, 10 << 20, 10 << 20, true},
		{"freeSpaceAbove not met", &lll.DefragRule{FreeSpaceAbove: q("1Gi")}, 500 << 20, 100 << 20, false},
		{"freeSpaceAbove met", &lll.DefragRule{FreeSpaceAbove: q("200Mi")}, 500 << 20, 100 << 20, true},
		{"quota arm: full but unfragmented never fires", &lll.DefragRule{QuotaUsageAbove: "80%"}, int64(1.9 * float64(gib)), int64(1.9 * float64(gib)), false},
		{"quota arm: under pressure with reclaimable fires", &lll.DefragRule{QuotaUsageAbove: "80%", MinReclaim: q("32Mi")}, int64(1.9 * float64(gib)), int64(1.9*float64(gib)) - (64 << 20), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := defragRuleTriggered(tc.rule, tc.dbSize, tc.dbSizeInUse, quota)
			if got != tc.want {
				t.Errorf("defragRuleTriggered = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── controller integration (fake etcd + fake kube client) ───────────────────

func defragEndpoint(name string) string { return fmt.Sprintf("http://%s.c1.ns.svc:2379", name) }

func defragCluster3() (*lll.EtcdCluster, []lll.EtcdMember) {
	cluster := &lll.EtcdCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"},
		Spec:       lll.EtcdClusterSpec{Replicas: ptrInt32(3), Version: "3.6.11", Storage: lll.StorageSpec{Size: resource.MustParse("1Gi")}},
		Status:     lll.EtcdClusterStatus{ClusterID: "abc", Observed: &lll.ObservedClusterSpec{Replicas: 3, Version: "3.6.11", Storage: lll.StorageSpec{Size: resource.MustParse("1Gi")}}},
	}
	var members []lll.EtcdMember
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("c1-%d", i)
		members = append(members, lll.EtcdMember{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: memberLabels("c1", name)},
			Spec:       lll.EtcdMemberSpec{ClusterName: "c1", Version: "3.6.11", Storage: lll.StorageSpec{Size: resource.MustParse("1Gi")}, InitialCluster: "x", ClusterToken: "t"},
			Status:     lll.EtcdMemberStatus{PodName: name},
		})
	}
	return cluster, members
}

func status(memberID, leader uint64, dbSize, inUse int64) *clientv3.StatusResponse {
	return &clientv3.StatusResponse{Header: &etcdserverpb.ResponseHeader{MemberId: memberID}, Leader: leader, DbSize: dbSize, DbSizeInUse: inUse}
}

func objs(cluster *lll.EtcdCluster, members []lll.EtcdMember, dfs ...*lll.EtcdDefrag) []client.Object {
	out := []client.Object{cluster}
	for i := range members {
		out = append(out, &members[i])
	}
	for _, d := range dfs {
		out = append(out, d)
	}
	return out
}

// driveDefrag reconciles d1 until it reaches a terminal phase or the cap.
func driveDefrag(t *testing.T, r *EtcdDefragReconciler, c client.Client, name string) *lll.EtcdDefrag {
	t.Helper()
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: nn(name, "ns")}
	for i := 0; i < 20; i++ {
		if _, err := r.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		df := &lll.EtcdDefrag{}
		if err := c.Get(ctx, nn(name, "ns"), df); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if df.Status.Phase == lll.EtcdDefragPhaseComplete || df.Status.Phase == lll.EtcdDefragPhaseFailed {
			return df
		}
	}
	df := &lll.EtcdDefrag{}
	_ = c.Get(ctx, nn(name, "ns"), df)
	return df
}

func nn(name, ns string) client.ObjectKey { return client.ObjectKey{Name: name, Namespace: ns} }

// A fragmented follower on a healthy cluster is defragmented; the leader and an
// unfragmented follower are skipped; the follower is done before the leader.
func TestEtcdDefrag_DefragmentsFragmentedMember(t *testing.T) {
	cluster, members := defragCluster3()
	df := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}}}
	c, s := newTestClient(t, objs(cluster, members, df)...)

	fe := newFakeEtcd(0xabc)
	fe.leader = 10
	fe.statusByEndpoint = map[string]*clientv3.StatusResponse{
		defragEndpoint("c1-0"): status(10, 10, 100<<20, 100<<20), // leader, clean
		defragEndpoint("c1-1"): status(11, 10, 500<<20, 100<<20), // follower, fragmented
		defragEndpoint("c1-2"): status(12, 10, 100<<20, 100<<20), // follower, clean
	}
	r := &EtcdDefragReconciler{Client: c, Scheme: s, EtcdClientFactory: factoryReturning(fe), Recorder: record.NewFakeRecorder(20)}

	got := driveDefrag(t, r, c, "d1")
	if got.Status.Phase != lll.EtcdDefragPhaseComplete {
		t.Fatalf("phase = %q, want Complete", got.Status.Phase)
	}
	if len(fe.defragCalls) != 1 || fe.defragCalls[0] != defragEndpoint("c1-1") {
		t.Fatalf("defragCalls = %v, want exactly [c1-1]", fe.defragCalls)
	}
	if got.Status.Defragmented != 1 {
		t.Errorf("Defragmented = %d, want 1", got.Status.Defragmented)
	}
	// Members recorded; the fragmented follower Defragmented, leader last.
	outcome := map[string]lll.DefragOutcome{}
	for _, m := range got.Status.Members {
		outcome[m.Name] = m.Outcome
	}
	if outcome["c1-1"] != lll.DefragOutcomeDefragmented {
		t.Errorf("c1-1 outcome = %q, want Defragmented", outcome["c1-1"])
	}
	if got.Status.Members[len(got.Status.Members)-1].Role != lll.MemberRoleLeader {
		t.Errorf("leader not last in the plan: %+v", got.Status.Members)
	}
}

// rule.all defragments every member unconditionally.
func TestEtcdDefrag_RuleAllDefragmentsEveryone(t *testing.T) {
	cluster, members := defragCluster3()
	df := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}, Rule: &lll.DefragRule{All: true}}}
	c, s := newTestClient(t, objs(cluster, members, df)...)
	fe := newFakeEtcd(0xabc)
	fe.leader = 10
	fe.statusByEndpoint = map[string]*clientv3.StatusResponse{
		defragEndpoint("c1-0"): status(10, 10, 50<<20, 50<<20),
		defragEndpoint("c1-1"): status(11, 10, 50<<20, 50<<20),
		defragEndpoint("c1-2"): status(12, 10, 50<<20, 50<<20),
	}
	r := &EtcdDefragReconciler{Client: c, Scheme: s, EtcdClientFactory: factoryReturning(fe), Recorder: record.NewFakeRecorder(20)}

	got := driveDefrag(t, r, c, "d1")
	if got.Status.Phase != lll.EtcdDefragPhaseComplete || got.Status.Defragmented != 3 {
		t.Fatalf("phase=%q defragmented=%d, want Complete/3", got.Status.Phase, got.Status.Defragmented)
	}
	if len(fe.defragCalls) != 3 {
		t.Fatalf("defragCalls = %v, want all 3", fe.defragCalls)
	}
	// Leader defragmented last.
	if fe.defragCalls[2] != defragEndpoint("c1-0") {
		t.Errorf("leader c1-0 not defragmented last: %v", fe.defragCalls)
	}
}

// Quorum lost (two members unreachable) → no defrag, phase Pending,
// DefragChecked=False/ClusterNotHealthy, a DefragDeferred event.
func TestEtcdDefrag_DeferredWhenUnhealthy(t *testing.T) {
	cluster, members := defragCluster3()
	df := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}}}
	c, s := newTestClient(t, objs(cluster, members, df)...)
	fe := newFakeEtcd(0xabc)
	fe.leader = 10
	fe.statusByEndpoint = map[string]*clientv3.StatusResponse{
		defragEndpoint("c1-0"): status(10, 10, 500<<20, 100<<20),
	}
	fe.statusErrByEndpoint = map[string]error{
		defragEndpoint("c1-1"): errors.New("context deadline exceeded"),
		defragEndpoint("c1-2"): errors.New("context deadline exceeded"),
	}
	rec := record.NewFakeRecorder(20)
	r := &EtcdDefragReconciler{Client: c, Scheme: s, EtcdClientFactory: factoryReturning(fe), Recorder: rec}

	ctx := context.Background()
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: nn("d1", "ns")})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected a requeue while deferred, got %+v", res)
	}
	if len(fe.defragCalls) != 0 {
		t.Fatalf("defrag ran on an unhealthy cluster: %v", fe.defragCalls)
	}
	got := &lll.EtcdDefrag{}
	if err := c.Get(ctx, nn("d1", "ns"), got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != lll.EtcdDefragPhasePending {
		t.Errorf("phase = %q, want Pending", got.Status.Phase)
	}
	cond := findDefragCond(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "ClusterNotHealthy" {
		t.Errorf("DefragChecked = %+v, want False/ClusterNotHealthy", cond)
	}
	assertDefragEvent(t, rec, "DefragDeferred")
}

// A failed Defragment RPC lands the run in Failed with the member marked Failed.
func TestEtcdDefrag_FailedRPC(t *testing.T) {
	cluster, members := defragCluster3()
	df := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}, Rule: &lll.DefragRule{All: true}}}
	c, s := newTestClient(t, objs(cluster, members, df)...)
	fe := newFakeEtcd(0xabc)
	fe.leader = 10
	fe.defragErr = errors.New("boom")
	fe.statusByEndpoint = map[string]*clientv3.StatusResponse{
		defragEndpoint("c1-0"): status(10, 10, 50<<20, 50<<20),
		defragEndpoint("c1-1"): status(11, 10, 50<<20, 50<<20),
		defragEndpoint("c1-2"): status(12, 10, 50<<20, 50<<20),
	}
	r := &EtcdDefragReconciler{Client: c, Scheme: s, EtcdClientFactory: factoryReturning(fe), Recorder: record.NewFakeRecorder(20)}

	got := driveDefrag(t, r, c, "d1")
	if got.Status.Phase != lll.EtcdDefragPhaseFailed {
		t.Fatalf("phase = %q, want Failed", got.Status.Phase)
	}
	failed := false
	for _, m := range got.Status.Members {
		if m.Outcome == lll.DefragOutcomeFailed {
			failed = true
		}
	}
	if !failed {
		t.Errorf("no member marked Failed: %+v", got.Status.Members)
	}
}

// Two EtcdDefrags for one cluster are serialized: only the oldest acts; the
// newer stays Pending and performs no defrag until the first finishes.
func TestEtcdDefrag_SerializedPerCluster(t *testing.T) {
	cluster, members := defragCluster3()
	older := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d-old", Namespace: "ns", CreationTimestamp: metav1.Unix(100, 0)}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}, Rule: &lll.DefragRule{All: true}}}
	newer := &lll.EtcdDefrag{ObjectMeta: metav1.ObjectMeta{Name: "d-new", Namespace: "ns", CreationTimestamp: metav1.Unix(200, 0)}, Spec: lll.EtcdDefragSpec{ClusterRef: corev1.LocalObjectReference{Name: "c1"}, Rule: &lll.DefragRule{All: true}}}
	c, s := newTestClient(t, objs(cluster, members, older, newer)...)
	fe := newFakeEtcd(0xabc)
	fe.leader = 10
	fe.statusByEndpoint = map[string]*clientv3.StatusResponse{
		defragEndpoint("c1-0"): status(10, 10, 50<<20, 50<<20),
		defragEndpoint("c1-1"): status(11, 10, 50<<20, 50<<20),
		defragEndpoint("c1-2"): status(12, 10, 50<<20, 50<<20),
	}
	r := &EtcdDefragReconciler{Client: c, Scheme: s, EtcdClientFactory: factoryReturning(fe), Recorder: record.NewFakeRecorder(20)}

	// Reconcile the NEWER one: it must stay Pending and not defrag.
	ctx := context.Background()
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: nn("d-new", "ns")}); err != nil {
		t.Fatalf("reconcile d-new: %v", err)
	}
	if len(fe.defragCalls) != 0 {
		t.Fatalf("newer EtcdDefrag defragged while an older one is active: %v", fe.defragCalls)
	}
	got := &lll.EtcdDefrag{}
	_ = c.Get(ctx, nn("d-new", "ns"), got)
	if got.Status.Phase != lll.EtcdDefragPhasePending {
		t.Errorf("d-new phase = %q, want Pending (queued)", got.Status.Phase)
	}
}

func findDefragCond(df *lll.EtcdDefrag) *metav1.Condition {
	for i := range df.Status.Conditions {
		if df.Status.Conditions[i].Type == "DefragChecked" {
			return &df.Status.Conditions[i]
		}
	}
	return nil
}

func assertDefragEvent(t *testing.T, rec *record.FakeRecorder, wantReason string) {
	t.Helper()
	select {
	case ev := <-rec.Events:
		if !strings.Contains(ev, wantReason) {
			t.Errorf("event = %q, want one mentioning %q", ev, wantReason)
		}
	default:
		t.Errorf("no event emitted, want one mentioning %q", wantReason)
	}
}
