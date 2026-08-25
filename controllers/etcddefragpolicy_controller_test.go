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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lll "github.com/cozystack/etcd-operator/api/v1alpha2"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestParseSchedule(t *testing.T) {
	if _, err := parseSchedule(lll.DefragSchedule{Cron: "0 3 * * *"}); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	// No timezone means UTC: "0 0 * * *" from 12:00 UTC lands on the next UTC
	// midnight regardless of the process zone.
	sched, err := parseSchedule(lll.DefragSchedule{Cron: "0 0 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	got := sched.Next(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Next = %s, want %s", got, want)
	}

	// A named zone shifts the boundary: 03:00 Europe/Moscow (UTC+3) is 00:00 UTC.
	msk, err := parseSchedule(lll.DefragSchedule{Cron: "0 3 * * *", Timezone: "Europe/Moscow"})
	if err != nil {
		t.Fatalf("named timezone rejected (is time/tzdata imported?): %v", err)
	}
	got = msk.Next(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Europe/Moscow Next = %s, want %s (00:00 UTC)", got, want)
	}

	if _, err := parseSchedule(lll.DefragSchedule{Cron: "not a schedule"}); err == nil {
		t.Error("expected an error for an unparseable schedule")
	}
	if _, err := parseSchedule(lll.DefragSchedule{Cron: "0 3 * * *", Timezone: "Mars/Olympus"}); err == nil {
		t.Error("expected an error for an unknown timezone")
	}
	// Descriptors are outside the documented five-field grammar.
	if _, err := parseSchedule(lll.DefragSchedule{Cron: "@hourly"}); err == nil {
		t.Error("expected @hourly to be rejected by the five-field parser")
	}
}

func TestNextSchedule(t *testing.T) {
	sched, err := parseSchedule(lll.DefragSchedule{Cron: "0 * * * *"}) // top of every hour
	if err != nil {
		t.Fatal(err)
	}

	// Next tick still in the future: nothing due.
	if due, next, err := nextSchedule(sched, epoch, epoch.Add(30*time.Minute), 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if due != nil {
		t.Errorf("due = %s, want nil (next tick is in the future)", due)
	} else if want := epoch.Add(time.Hour); !next.Equal(want) {
		t.Errorf("next = %s, want %s", next, want)
	}

	// One tick due: the most recent boundary at or before now.
	if due, next, err := nextSchedule(sched, epoch, epoch.Add(90*time.Minute), 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if due == nil {
		t.Fatal("due = nil, want the 01:00 tick")
	} else if !due.Equal(epoch.Add(time.Hour)) {
		t.Errorf("due = %s, want %s", due, epoch.Add(time.Hour))
	} else if !next.Equal(epoch.Add(2 * time.Hour)) {
		t.Errorf("next = %s, want %s", next, epoch.Add(2*time.Hour))
	}

	// A backlog longer than maxCatchup returns an error rather than fabricating a
	// non-boundary tick — the guard that finding 1 was about.
	if _, _, err := nextSchedule(sched, epoch, epoch.Add(1000*time.Hour), 100); !errors.Is(err, errTooManyMissed) {
		t.Errorf("err = %v, want errTooManyMissed for a backlog past maxCatchup", err)
	}
}

// ── controller ──────────────────────────────────────────────────────────────

func defragPolicy(name, schedule string, opts ...func(*lll.EtcdDefragPolicy)) *lll.EtcdDefragPolicy {
	p := &lll.EtcdDefragPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: policyUID(name), CreationTimestamp: metav1.NewTime(epoch)},
		Spec: lll.EtcdDefragPolicySpec{
			ClusterRef: corev1.LocalObjectReference{Name: "c1"},
			Schedule:   lll.DefragSchedule{Cron: schedule},
		},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func policyUID(name string) types.UID { return types.UID(name + "-uid") }

// ownedBy is the controller ownerRef a stamped run carries, matching what
// SetControllerReference writes, so ownedRuns' IsControlledBy check keeps it.
func ownedBy(policy string) []metav1.OwnerReference {
	controller := true
	return []metav1.OwnerReference{{
		APIVersion: lll.GroupVersion.String(),
		Kind:       "EtcdDefragPolicy",
		Name:       policy,
		UID:        policyUID(policy),
		Controller: &controller,
	}}
}

func policyReconciler(t *testing.T, now time.Time, objs ...client.Object) (*EtcdDefragPolicyReconciler, client.Client) {
	t.Helper()
	c, s := newTestClient(t, objs...)
	return &EtcdDefragPolicyReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(20), now: func() time.Time { return now }}, c
}

func listPolicyRuns(t *testing.T, c client.Client, policy string) []lll.EtcdDefrag {
	t.Helper()
	var runs lll.EtcdDefragList
	if err := c.List(context.Background(), &runs, client.InNamespace("ns"), client.MatchingLabels{LabelDefragPolicy: policy}); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	return runs.Items
}

func reconcilePolicy(t *testing.T, r *EtcdDefragPolicyReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn(name, "ns")})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

// A due tick stamps one EtcdDefrag, owned by and labelled with the policy,
// carrying the policy's rule/ttl, and records lastScheduleTime.
func TestDefragPolicy_StampsWhenDue(t *testing.T) {
	ttl := int32(3600)
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) {
		p.Spec.Rule = &lll.DefragRule{All: true}
		p.Spec.TTLSecondsAfterFinished = &ttl
	})
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol)

	reconcilePolicy(t, r, "p")

	runs := listPolicyRuns(t, c, "p")
	if len(runs) != 1 {
		t.Fatalf("stamped %d runs, want 1", len(runs))
	}
	run := runs[0]
	if run.Spec.ClusterRef.Name != "c1" || run.Spec.Rule == nil || !run.Spec.Rule.All {
		t.Errorf("stamped run spec = %+v, want clusterRef c1 + rule.all", run.Spec)
	}
	if run.Spec.TTLSecondsAfterFinished == nil || *run.Spec.TTLSecondsAfterFinished != ttl {
		t.Errorf("stamped ttl = %v, want %d", run.Spec.TTLSecondsAfterFinished, ttl)
	}
	if run.Labels[LabelCluster] != "c1" {
		t.Errorf("missing cluster label: %v", run.Labels)
	}
	if len(run.OwnerReferences) != 1 || run.OwnerReferences[0].Name != "p" {
		t.Errorf("owner refs = %+v, want the policy", run.OwnerReferences)
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if got.Status.LastScheduleTime == nil || !got.Status.LastScheduleTime.Time.Equal(epoch.Add(time.Hour)) {
		t.Errorf("lastScheduleTime = %v, want 01:00", got.Status.LastScheduleTime)
	}
	if len(got.Status.Active) != 1 {
		t.Errorf("status.active = %v, want the stamped run", got.Status.Active)
	}
}

// Before the first tick, nothing is stamped and the policy requeues.
func TestDefragPolicy_NotDueYet(t *testing.T) {
	pol := defragPolicy("p", "0 0 * * *") // daily midnight
	r, c := policyReconciler(t, epoch.Add(time.Hour), pol)

	res := reconcilePolicy(t, r, "p")
	if len(listPolicyRuns(t, c, "p")) != 0 {
		t.Fatalf("stamped a run before the first tick")
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("expected a requeue toward the next tick, got %+v", res)
	}
}

// A suspended policy stamps nothing and reports Suspended.
func TestDefragPolicy_Suspended(t *testing.T) {
	suspend := true
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.Suspend = &suspend })
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol)

	reconcilePolicy(t, r, "p")
	if len(listPolicyRuns(t, c, "p")) != 0 {
		t.Fatalf("suspended policy stamped a run")
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if cond := findPolicyCond(got); cond == nil || cond.Reason != "Suspended" {
		t.Errorf("condition = %+v, want Suspended", cond)
	}
}

// An unparseable schedule reports InvalidSchedule and stamps nothing.
func TestDefragPolicy_InvalidSchedule(t *testing.T) {
	pol := defragPolicy("p", "every blue moon")
	r, c := policyReconciler(t, epoch.Add(time.Hour), pol)

	reconcilePolicy(t, r, "p")
	if len(listPolicyRuns(t, c, "p")) != 0 {
		t.Fatalf("stamped a run on an invalid schedule")
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if cond := findPolicyCond(got); cond == nil || cond.Reason != "InvalidSchedule" {
		t.Errorf("condition = %+v, want InvalidSchedule", cond)
	}
}

// With the default Forbid policy, a due tick is skipped while a previous run is
// still active — no second run is stamped, but the tick is consumed.
func TestDefragPolicy_ForbidConcurrent(t *testing.T) {
	pol := defragPolicy("p", "0 * * * *")
	active := activeRun("p-existing", "p")
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol, active)

	reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 1 {
		t.Fatalf("Forbid stamped a concurrent run: %d runs", len(runs))
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if got.Status.LastScheduleTime == nil {
		t.Errorf("a forbidden tick should still advance lastScheduleTime")
	}
}

// With Allow, a due tick is stamped even while a previous run is active.
func TestDefragPolicy_AllowConcurrent(t *testing.T) {
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.ConcurrencyPolicy = lll.AllowConcurrent })
	active := activeRun("p-existing", "p")
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol, active)

	reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 2 {
		t.Fatalf("Allow did not stamp a concurrent run: %d runs", len(runs))
	}
}

// A tick older than StartingDeadlineSeconds is never started late: the deadline
// bounds the catch-up window, so the stale 01:00 tick falls outside it and
// nothing is stamped. The next in-window tick will run on its own reconcile.
func TestDefragPolicy_MissedDeadline(t *testing.T) {
	deadline := int64(60)
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.StartingDeadlineSeconds = &deadline })
	// now is 01:30, so the 01:00 tick is 30m old — well past the 60s deadline.
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol)

	res := reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 0 {
		t.Fatalf("stamped a run past the starting deadline: %d runs", len(runs))
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("expected a requeue toward the next tick, got %+v", res)
	}
}

// A policy arbitrarily far behind resumes at its most recent tick rather than
// parking: with no deadline the lookback floor bounds the walk, so one run is
// stamped for the latest tick and the skipped backlog is reported as an event.
// Parking instead would never recover, since nothing advances the anchor.
func TestDefragPolicy_FarBehindResumes(t *testing.T) {
	pol := defragPolicy("p", "* * * * *") // every minute: 1000h backlog >> maxCatchup
	rec := record.NewFakeRecorder(20)
	c, s := newTestClient(t, pol)
	now := epoch.Add(1000 * time.Hour)
	r := &EtcdDefragPolicyReconciler{Client: c, Scheme: s, Recorder: rec, now: func() time.Time { return now }}

	reconcilePolicy(t, r, "p")
	runs := listPolicyRuns(t, c, "p")
	if len(runs) != 1 {
		t.Fatalf("stamped %d runs on a large backlog, want 1 (the most recent tick)", len(runs))
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if cond := findPolicyCond(got); cond == nil || cond.Reason != "Scheduled" {
		t.Errorf("condition = %+v, want Scheduled (the policy must not park)", cond)
	}
	if got.Status.LastScheduleTime == nil || !got.Status.LastScheduleTime.Time.Equal(now) {
		t.Errorf("lastScheduleTime = %v, want the latest tick %s", got.Status.LastScheduleTime, now)
	}
	if !drainFor(rec, "MissedSchedule") {
		t.Error("skipping a backlog should emit a MissedSchedule warning, not pass silently")
	}

	// The anchor advanced, so a second pass is a no-op rather than a repeat.
	reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 1 {
		t.Errorf("second pass stamped again: %d runs, want 1", len(runs))
	}
}

// A tick dropped by StartingDeadlineSeconds is reported, not silently swallowed:
// otherwise the policy reads Scheduled with no trace of the run that never was.
func TestDefragPolicy_MissedDeadlineIsReported(t *testing.T) {
	deadline := int64(60)
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.StartingDeadlineSeconds = &deadline })
	rec := record.NewFakeRecorder(20)
	c, s := newTestClient(t, pol)
	r := &EtcdDefragPolicyReconciler{Client: c, Scheme: s, Recorder: rec, now: func() time.Time { return epoch.Add(90 * time.Minute) }}

	reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 0 {
		t.Fatalf("stamped a run past the starting deadline: %d runs", len(runs))
	}
	if !drainFor(rec, "MissedSchedule") {
		t.Error("a deadline-skipped tick should emit a MissedSchedule warning")
	}
}

// drainFor reports whether any buffered event mentions reason.
func drainFor(rec *record.FakeRecorder, reason string) bool {
	for {
		select {
		case e := <-rec.Events:
			if strings.Contains(e, reason) {
				return true
			}
		default:
			return false
		}
	}
}

// A tick within StartingDeadlineSeconds is stamped normally: the deadline only
// suppresses runs older than its window.
func TestDefragPolicy_WithinDeadline(t *testing.T) {
	deadline := int64(7200) // 2h, comfortably wider than the 30m-old tick
	pol := defragPolicy("p", "0 * * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.StartingDeadlineSeconds = &deadline })
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol)

	reconcilePolicy(t, r, "p")
	if runs := listPolicyRuns(t, c, "p"); len(runs) != 1 {
		t.Fatalf("a tick within the deadline should stamp one run, got %d", len(runs))
	}
}

// HistoryLimit trims the oldest finished runs, keeping the newest.
func TestDefragPolicy_HistoryLimit(t *testing.T) {
	limit := int32(1)
	pol := defragPolicy("p", "0 0 * * *", func(p *lll.EtcdDefragPolicy) { p.Spec.HistoryLimit = &limit }) // not due
	old1 := finishedRun("p-1", "p", epoch.Add(1*time.Hour))
	old2 := finishedRun("p-2", "p", epoch.Add(2*time.Hour))
	newest := finishedRun("p-3", "p", epoch.Add(3*time.Hour))
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol, old1, old2, newest)

	reconcilePolicy(t, r, "p")
	runs := listPolicyRuns(t, c, "p")
	if len(runs) != 1 {
		t.Fatalf("history GC kept %d runs, want 1", len(runs))
	}
	if runs[0].Name != "p-3" {
		t.Errorf("GC kept %q, want the newest p-3", runs[0].Name)
	}
}

// An EtcdDefrag that carries the policy label but is not controlled by the
// policy (e.g. hand-copied YAML) is ignored: it must not count as active under
// Forbid, so the due tick still stamps a genuine run.
func TestDefragPolicy_IgnoresUnownedLabelledRun(t *testing.T) {
	pol := defragPolicy("p", "0 * * * *") // default Forbid
	imposter := &lll.EtcdDefrag{
		ObjectMeta: metav1.ObjectMeta{Name: "hand-copied", Namespace: "ns", Labels: map[string]string{LabelDefragPolicy: "p"}},
		Status:     lll.EtcdDefragStatus{Phase: lll.EtcdDefragPhaseRunning},
	}
	r, c := policyReconciler(t, epoch.Add(90*time.Minute), pol, imposter)

	reconcilePolicy(t, r, "p")
	stamped := 0
	for _, run := range listPolicyRuns(t, c, "p") {
		if run.Name != "hand-copied" {
			stamped++
		}
	}
	if stamped != 1 {
		t.Fatalf("owned runs stamped = %d, want 1 (the imposter must not block Forbid)", stamped)
	}
	got := mustGet(t, c, "p", "ns", &lll.EtcdDefragPolicy{})
	if len(got.Status.Active) != 1 {
		t.Errorf("status.active = %v, want only the owned run", got.Status.Active)
	}
}

// A rejected Create surfaces on the Active condition rather than only in logs.
func TestDefragPolicy_StampFailedSurfacesCondition(t *testing.T) {
	pol := defragPolicy("p", "0 * * * *")
	base, s := newTestClient(t, pol)
	r := &EtcdDefragPolicyReconciler{
		Client:   &createFailClient{Client: base, err: errors.New("admission rejected")},
		Scheme:   s,
		Recorder: record.NewFakeRecorder(20),
		now:      func() time.Time { return epoch.Add(90 * time.Minute) },
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn("p", "ns")}); err == nil {
		t.Fatal("expected the create error to propagate")
	}
	got := mustGet(t, base, "p", "ns", &lll.EtcdDefragPolicy{})
	if cond := findPolicyCond(got); cond == nil || cond.Reason != "StampFailed" {
		t.Errorf("condition = %+v, want StampFailed", cond)
	}
}

// createFailClient fails every Create with a preset error, driving the
// rejected-stamp path without an apiserver.
type createFailClient struct {
	client.Client
	err error
}

func (c *createFailClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.err
}

func activeRun(name, policy string) *lll.EtcdDefrag {
	return &lll.EtcdDefrag{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: map[string]string{LabelDefragPolicy: policy}, OwnerReferences: ownedBy(policy)},
		Status:     lll.EtcdDefragStatus{Phase: lll.EtcdDefragPhaseRunning},
	}
}

func finishedRun(name, policy string, completed time.Time) *lll.EtcdDefrag {
	ct := metav1.NewTime(completed)
	return &lll.EtcdDefrag{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: map[string]string{LabelDefragPolicy: policy}, OwnerReferences: ownedBy(policy)},
		Status:     lll.EtcdDefragStatus{Phase: lll.EtcdDefragPhaseComplete, CompletedAt: &ct},
	}
}

func findPolicyCond(pol *lll.EtcdDefragPolicy) *metav1.Condition {
	for i := range pol.Status.Conditions {
		if pol.Status.Conditions[i].Type == defragPolicyCondition {
			return &pol.Status.Conditions[i]
		}
	}
	return nil
}
