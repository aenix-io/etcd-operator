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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	lll "github.com/cozystack/etcd-operator/api/v1alpha2"
)

const (
	// Rule defaults (see api DefragRule). A defrag can only reclaim
	// DbSize-DbSizeInUse, so freeSpaceAbove is the always-applied floor.
	defaultDefragFreeSpace  = int64(200 << 20) // 200Mi
	defaultDefragMinReclaim = int64(32 << 20)  // 32Mi
	// defaultEtcdQuotaBytes mirrors etcd's built-in --quota-backend-bytes
	// default (2Gi), used when the cluster leaves spec.options.quotaBackendBytes
	// unset.
	defaultEtcdQuotaBytes = int64(2 << 30)

	// defragStatusTimeout bounds a per-member Maintenance Status probe;
	// defragRPCTimeout bounds a single stop-the-world Defragment call.
	defragStatusTimeout = 5 * time.Second
	defragRPCTimeout    = 5 * time.Minute
	// defragRequeueAfter paces the one-member-per-pass sweep and the retry while
	// deferred on an unhealthy cluster.
	defragRequeueAfter = 10 * time.Second
	// defragActiveDeadline bounds a whole run (Running + waiting-while-Pending),
	// so a run stuck on an unhealthy cluster can't hold the per-cluster slot
	// forever.
	defragActiveDeadline = 30 * time.Minute
)

// EtcdDefragReconciler drives an EtcdDefrag: a one-shot, run-to-completion
// defragmentation of an EtcdCluster's members. It defragments members one at a
// time (followers before the leader) on a healthy cluster, deferring rather
// than forcing while the cluster is degraded, and records per-member outcomes
// in status.
type EtcdDefragReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// EtcdClientFactory builds an etcd client; tests inject a fake.
	EtcdClientFactory EtcdClientFactory

	// Recorder emits events (e.g. when a due defrag is deferred). Tests may
	// leave it nil.
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=etcd-operator.cozystack.io,resources=etcddefrags,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=etcd-operator.cozystack.io,resources=etcddefrags/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=etcd-operator.cozystack.io,resources=etcdclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=etcd-operator.cozystack.io,resources=etcdmembers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *EtcdDefragReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	df := &lll.EtcdDefrag{}
	if err := r.Get(ctx, req.NamespacedName, df); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Terminal: only TTL GC remains.
	if df.Status.Phase == lll.EtcdDefragPhaseComplete || df.Status.Phase == lll.EtcdDefragPhaseFailed {
		return r.handleTTL(ctx, df)
	}

	cluster := &lll.EtcdCluster{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: df.Namespace, Name: df.Spec.ClusterRef.Name}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return r.fail(ctx, df, "ClusterNotFound",
				fmt.Sprintf("EtcdCluster %q not found in namespace %q", df.Spec.ClusterRef.Name, df.Namespace))
		}
		return ctrl.Result{}, err
	}

	// Serialize per cluster: only the oldest non-terminal EtcdDefrag targeting
	// this cluster may act; the rest wait in Pending.
	if active, err := r.oldestActive(ctx, df.Namespace, cluster.Name); err != nil {
		return ctrl.Result{}, err
	} else if active != "" && active != df.Name {
		if setDefragCondition(df, metav1.ConditionFalse, "Queued",
			fmt.Sprintf("waiting for EtcdDefrag %q to finish", active)) || df.Status.Phase != lll.EtcdDefragPhasePending {
			df.Status.Phase = lll.EtcdDefragPhasePending
			if err := r.Status().Update(ctx, df); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: defragRequeueAfter}, nil
	}

	// Overall deadline: a run that can't make progress must fail, not linger.
	// (CreationTimestamp is zero before the apiserver stamps it — e.g. in unit
	// tests — so only enforce against a non-zero reference time.)
	if started := df.Status.StartedAt; started != nil && time.Since(started.Time) > defragActiveDeadline {
		return r.fail(ctx, df, "DeadlineExceeded",
			fmt.Sprintf("defragmentation did not complete within %s", defragActiveDeadline))
	} else if started == nil && !df.CreationTimestamp.IsZero() && time.Since(df.CreationTimestamp.Time) > defragActiveDeadline {
		return r.fail(ctx, df, "DeadlineExceeded",
			fmt.Sprintf("defragmentation could not start within %s (cluster never became healthy)", defragActiveDeadline))
	}

	var memberList lll.EtcdMemberList
	if err := r.List(ctx, &memberList, client.InNamespace(df.Namespace),
		client.MatchingLabels{LabelCluster: cluster.Name}); err != nil {
		return ctrl.Result{}, err
	}
	running := filterRunningMembers(memberList.Items)

	c, backends, err := r.dialAndProbe(ctx, cluster, running)
	if err != nil {
		logger.Error(err, "defrag: cannot dial/probe cluster; retrying")
		return ctrl.Result{RequeueAfter: defragRequeueAfter}, nil
	}
	defer c.Close()

	// Health gate: defrag is only safe when every desired member is present,
	// reachable, alarm-free and agrees on a leader.
	if reason, ok := clusterDefragHealthy(cluster, running, backends); !ok {
		msg := "a defragmentation is due but the cluster is not fully healthy; deferring to protect quorum: " + reason
		if setDefragCondition(df, metav1.ConditionFalse, "ClusterNotHealthy", msg) {
			r.event(df, corev1.EventTypeWarning, "DefragDeferred", msg)
		}
		df.Status.Phase = lll.EtcdDefragPhasePending
		if err := r.Status().Update(ctx, df); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defragRequeueAfter}, nil
	}

	// Initialize the per-member work list once (followers first, leader last).
	if len(df.Status.Members) == 0 {
		df.Status.Members = plannedMembers(backends)
	}

	next := firstPendingMember(df)
	if next == nil {
		return r.finalize(ctx, df)
	}

	b := backendByName(backends, next.Name)
	if b == nil {
		// A planned member vanished between passes despite the health gate.
		markMember(df, next.Name, lll.DefragOutcomeFailed, "MemberGone", nil)
		return r.persistAndRequeue(ctx, df)
	}

	if df.Status.Phase != lll.EtcdDefragPhaseRunning {
		df.Status.Phase = lll.EtcdDefragPhaseRunning
		now := metav1.Now()
		df.Status.StartedAt = &now
		setDefragCondition(df, metav1.ConditionTrue, "Running", "defragmenting members")
	}

	quota := effectiveQuotaBytes(cluster)
	if trig, reason := defragRuleTriggered(df.Spec.Rule, b.status.DbSize, b.status.DbSizeInUse, quota); !trig {
		markMember(df, b.member.Name, lll.DefragOutcomeSkipped, reason, b)
		return r.persistAndRequeue(ctx, df)
	}

	dctx, cancel := context.WithTimeout(ctx, defragRPCTimeout)
	defer cancel()
	if _, derr := c.Defragment(dctx, b.endpoint); derr != nil {
		msg := fmt.Sprintf("defragmentation of member %s failed: %v", b.member.Name, derr)
		markMember(df, b.member.Name, lll.DefragOutcomeFailed, "RPCError", b)
		r.event(df, corev1.EventTypeWarning, "DefragFailed", msg)
		logger.Error(derr, "defrag: member Defragment failed", "member", b.member.Name)
		return r.persistAndRequeue(ctx, df)
	}

	// Read the post-defrag size for the record (best-effort).
	if after, serr := statusWithTimeout(ctx, c, b.endpoint); serr == nil {
		b.after = after.DbSize
	}
	markMember(df, b.member.Name, lll.DefragOutcomeDefragmented, "", b)
	df.Status.Defragmented++
	logger.Info("defragmented member", "member", b.member.Name, "reclaimed", b.status.DbSize-b.after)
	return r.persistAndRequeue(ctx, df)
}

// memberBackend pairs a running member with its live Maintenance Status.
type memberBackend struct {
	member   *lll.EtcdMember
	endpoint string
	status   *clientv3.StatusResponse
	after    int64 // DbSize after a successful defrag
}

// dialAndProbe opens one client and reads every running member's Status. The
// caller closes the client.
func (r *EtcdDefragReconciler) dialAndProbe(ctx context.Context, cluster *lll.EtcdCluster, running []lll.EtcdMember) (EtcdClusterClient, []memberBackend, error) {
	tlsCfg, err := buildOperatorTLSConfig(ctx, r.Client, cluster)
	if err != nil {
		return nil, nil, err
	}
	user, pass, _, err := resolveEtcdCredentials(ctx, r.Client, cluster)
	if err != nil {
		return nil, nil, err
	}
	scheme := clusterClientScheme(cluster)
	endpoints := make([]string, len(running))
	for i := range running {
		endpoints[i] = clientURL(scheme, running[i].Name, memberServiceName(&running[i]), cluster.Namespace)
	}
	c, err := r.EtcdClientFactory(ctx, endpoints, tlsCfg, user, pass)
	if err != nil {
		return nil, nil, err
	}
	backends := make([]memberBackend, 0, len(running))
	for i := range running {
		resp, serr := statusWithTimeout(ctx, c, endpoints[i])
		if serr != nil {
			// Unreachable member: record a nil-status backend so the health gate
			// sees the gap.
			backends = append(backends, memberBackend{member: &running[i], endpoint: endpoints[i]})
			continue
		}
		backends = append(backends, memberBackend{member: &running[i], endpoint: endpoints[i], status: resp, after: resp.DbSize})
	}
	return c, backends, nil
}

func statusWithTimeout(ctx context.Context, c EtcdClusterClient, endpoint string) (*clientv3.StatusResponse, error) {
	sctx, cancel := context.WithTimeout(ctx, defragStatusTimeout)
	defer cancel()
	return c.Status(sctx, endpoint)
}

// clusterDefragHealthy reports whether the cluster is safe to defragment: every
// desired member present and reachable, alarm-free, and agreeing on a single
// non-zero leader. Status is a local read — a member answers it while
// partitioned or alarmed — so agreement and Errors are checked, not just that
// the RPC returned.
func clusterDefragHealthy(cluster *lll.EtcdCluster, running []lll.EtcdMember, backends []memberBackend) (string, bool) {
	desired := 0
	if cluster.Status.Observed != nil {
		desired = int(cluster.Status.Observed.Replicas)
	}
	if desired == 0 || len(running) != desired {
		return fmt.Sprintf("have %d running members, want %d", len(running), desired), false
	}
	var leader uint64
	for i := range backends {
		b := &backends[i]
		if b.status == nil {
			return fmt.Sprintf("member %s is unreachable", b.member.Name), false
		}
		if len(b.status.Errors) > 0 {
			return fmt.Sprintf("member %s reports alarms: %s", b.member.Name, strings.Join(b.status.Errors, ",")), false
		}
		if b.status.Leader == 0 {
			return fmt.Sprintf("member %s reports no leader", b.member.Name), false
		}
		if leader == 0 {
			leader = b.status.Leader
		} else if b.status.Leader != leader {
			return "members disagree on the leader", false
		}
	}
	return "", true
}

// plannedMembers builds the ordered work list: followers first, the leader
// last (its defrag is the most disruptive and is done only after followers
// prove defrag is healthy on this cluster).
func plannedMembers(backends []memberBackend) []lll.MemberDefragStatus {
	followers := make([]lll.MemberDefragStatus, 0, len(backends))
	var leader []lll.MemberDefragStatus
	for i := range backends {
		b := &backends[i]
		row := lll.MemberDefragStatus{Name: b.member.Name, Outcome: lll.DefragOutcomePending}
		if isLeaderStatus(b.status) {
			row.Role = lll.MemberRoleLeader
			leader = append(leader, row)
		} else {
			row.Role = lll.MemberRoleFollower
			followers = append(followers, row)
		}
	}
	return append(followers, leader...)
}

func isLeaderStatus(s *clientv3.StatusResponse) bool {
	return s != nil && s.Header != nil && s.Leader != 0 && s.Leader == s.Header.MemberId
}

func firstPendingMember(df *lll.EtcdDefrag) *lll.MemberDefragStatus {
	for i := range df.Status.Members {
		if df.Status.Members[i].Outcome == lll.DefragOutcomePending {
			return &df.Status.Members[i]
		}
	}
	return nil
}

func backendByName(backends []memberBackend, name string) *memberBackend {
	for i := range backends {
		if backends[i].member.Name == name {
			return &backends[i]
		}
	}
	return nil
}

func markMember(df *lll.EtcdDefrag, name string, outcome lll.DefragOutcome, reason string, b *memberBackend) {
	for i := range df.Status.Members {
		m := &df.Status.Members[i]
		if m.Name != name {
			continue
		}
		m.Outcome = outcome
		m.Reason = reason
		now := metav1.Now()
		m.FinishedAt = &now
		if b != nil && b.status != nil {
			m.DBSizeBefore = b.status.DbSize
			m.DBSizeAfter = b.after
			if outcome == lll.DefragOutcomeDefragmented && b.status.DbSize >= b.after {
				m.ReclaimedBytes = b.status.DbSize - b.after
			}
		}
		return
	}
}

// effectiveQuotaBytes is the backend quota the cluster's members run with: the
// latched spec.options.quotaBackendBytes, or etcd's 2Gi default.
func effectiveQuotaBytes(cluster *lll.EtcdCluster) int64 {
	if o := cluster.Status.Observed; o != nil && o.Options != nil &&
		o.Options.QuotaBackendBytes != nil && *o.Options.QuotaBackendBytes > 0 {
		return *o.Options.QuotaBackendBytes
	}
	return defaultEtcdQuotaBytes
}

// defragRuleTriggered reports whether a member's backend meets the rule, and a
// reason when it does not. rule.All is unconditional. Otherwise the free-space
// arm (reclaimable > freeSpaceAbove, default 200Mi) is always applied; the
// quota arm additionally fires under quota pressure but only when there is at
// least MinReclaim to reclaim — so a full-but-unfragmented backend is never
// defragmented for nothing.
func defragRuleTriggered(rule *lll.DefragRule, dbSize, dbSizeInUse, quota int64) (bool, string) {
	if rule != nil && rule.All {
		return true, ""
	}

	free := defaultDefragFreeSpace
	minReclaim := defaultDefragMinReclaim
	quotaUsage := 0.0
	quotaSet := false
	if rule != nil {
		if rule.FreeSpaceAbove != nil {
			free = rule.FreeSpaceAbove.Value()
		}
		if rule.MinReclaim != nil {
			minReclaim = rule.MinReclaim.Value()
		}
		if p, ok := parsePercent(rule.QuotaUsageAbove); ok {
			quotaUsage = p
			quotaSet = true
		}
	}

	reclaimable := dbSize - dbSizeInUse
	if reclaimable > free {
		return true, ""
	}
	if quotaSet && quota > 0 && float64(dbSize) > quotaUsage*float64(quota) && reclaimable >= minReclaim {
		return true, ""
	}
	return false, "BelowThreshold"
}

// parsePercent parses "80%" into 0.80. Returns ok=false for anything outside
// 1–100 or non-numeric; the CRD pattern rejects such values at admission, so
// this is a defensive fallback.
func parsePercent(s string) (float64, bool) {
	n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
	if err != nil || n <= 0 || n > 100 {
		return 0, false
	}
	return float64(n) / 100, true
}

// oldestActive returns the name of the oldest non-terminal EtcdDefrag targeting
// clusterName in namespace (creationTimestamp, name tiebreak), or "" if none.
func (r *EtcdDefragReconciler) oldestActive(ctx context.Context, namespace, clusterName string) (string, error) {
	var list lll.EtcdDefragList
	if err := r.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return "", err
	}
	active := make([]lll.EtcdDefrag, 0, len(list.Items))
	for _, d := range list.Items {
		if d.Spec.ClusterRef.Name != clusterName {
			continue
		}
		if d.Status.Phase == lll.EtcdDefragPhaseComplete || d.Status.Phase == lll.EtcdDefragPhaseFailed {
			continue
		}
		active = append(active, d)
	}
	if len(active) == 0 {
		return "", nil
	}
	sort.Slice(active, func(i, j int) bool {
		if !active[i].CreationTimestamp.Equal(&active[j].CreationTimestamp) {
			return active[i].CreationTimestamp.Before(&active[j].CreationTimestamp)
		}
		return active[i].Name < active[j].Name
	})
	return active[0].Name, nil
}

func (r *EtcdDefragReconciler) finalize(ctx context.Context, df *lll.EtcdDefrag) (ctrl.Result, error) {
	phase := lll.EtcdDefragPhaseComplete
	reason := "Complete"
	for _, m := range df.Status.Members {
		if m.Outcome == lll.DefragOutcomeFailed {
			phase = lll.EtcdDefragPhaseFailed
			reason = "MemberFailed"
			break
		}
	}
	df.Status.Phase = phase
	now := metav1.Now()
	df.Status.CompletedAt = &now
	status := metav1.ConditionTrue
	if phase == lll.EtcdDefragPhaseFailed {
		status = metav1.ConditionFalse
	}
	setDefragCondition(df, status, reason,
		fmt.Sprintf("%d/%d members defragmented", df.Status.Defragmented, len(df.Status.Members)))
	if err := r.Status().Update(ctx, df); err != nil {
		return ctrl.Result{}, err
	}
	return r.handleTTL(ctx, df)
}

func (r *EtcdDefragReconciler) fail(ctx context.Context, df *lll.EtcdDefrag, reason, msg string) (ctrl.Result, error) {
	df.Status.Phase = lll.EtcdDefragPhaseFailed
	now := metav1.Now()
	df.Status.CompletedAt = &now
	if setDefragCondition(df, metav1.ConditionFalse, reason, msg) {
		r.event(df, corev1.EventTypeWarning, reason, msg)
	}
	if err := r.Status().Update(ctx, df); err != nil {
		return ctrl.Result{}, err
	}
	return r.handleTTL(ctx, df)
}

func (r *EtcdDefragReconciler) persistAndRequeue(ctx context.Context, df *lll.EtcdDefrag) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, df); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: defragRequeueAfter}, nil
}

// handleTTL deletes a finished EtcdDefrag once TTLSecondsAfterFinished has
// elapsed, or requeues to delete it later. No TTL means keep it as history.
func (r *EtcdDefragReconciler) handleTTL(ctx context.Context, df *lll.EtcdDefrag) (ctrl.Result, error) {
	if df.Spec.TTLSecondsAfterFinished == nil || df.Status.CompletedAt == nil {
		return ctrl.Result{}, nil
	}
	expiry := df.Status.CompletedAt.Add(time.Duration(*df.Spec.TTLSecondsAfterFinished) * time.Second)
	if now := time.Now(); now.Before(expiry) {
		return ctrl.Result{RequeueAfter: expiry.Sub(now)}, nil
	}
	if err := r.Delete(ctx, df); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return ctrl.Result{}, nil
}

func (r *EtcdDefragReconciler) event(obj client.Object, eventType, reason, msg string) {
	if r.Recorder != nil {
		r.Recorder.Event(obj, eventType, reason, msg)
	}
}

// setDefragCondition upserts the DefragChecked condition and reports whether it
// changed — so callers emit an event only on a real transition, not every pass.
func setDefragCondition(df *lll.EtcdDefrag, status metav1.ConditionStatus, reason, msg string) bool {
	want := metav1.Condition{
		Type:               "DefragChecked",
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: df.Generation,
	}
	for _, existing := range df.Status.Conditions {
		if existing.Type == want.Type {
			if existing.Status == want.Status && existing.Reason == want.Reason &&
				existing.Message == want.Message && existing.ObservedGeneration == want.ObservedGeneration {
				return false
			}
			break
		}
	}
	setCondition(&df.Status.Conditions, want)
	return true
}

func (r *EtcdDefragReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.EtcdClientFactory == nil {
		r.EtcdClientFactory = DefaultEtcdClientFactory
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&lll.EtcdDefrag{}).
		Complete(r)
}
