/*
Copyright 2023 Timofey Larkin.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha2_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lll "github.com/cozystack/etcd-operator/api/v1alpha2"
)

func defragPolicy(name string) *lll.EtcdDefragPolicy {
	return &lll.EtcdDefragPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: lll.EtcdDefragPolicySpec{
			ClusterRef: corev1.LocalObjectReference{Name: "c1"},
			Schedule:   lll.DefragSchedule{Cron: "0 3 * * *"},
		},
	}
}

// The policy name becomes a label value on every stamped EtcdDefrag, and the
// controller selects its own runs by that label. A name past the 63-character
// label cap makes that selector unparseable, so the reconcile fails on its
// first List — before it can set any condition. Reject it at admission instead.
func TestCEL_DefragPolicyNameLength(t *testing.T) {
	skipIfNoEnvtest(t)
	ctx := context.Background()

	t.Run("at the cap accepted", func(t *testing.T) {
		p := defragPolicy(strings.Repeat("a", 52))
		if err := k8s.Create(ctx, p); err != nil {
			t.Fatalf("apiserver rejected a 52-character name: %v", err)
		}
		_ = k8s.Delete(ctx, p)
	})

	t.Run("past the cap rejected", func(t *testing.T) {
		p := defragPolicy(strings.Repeat("b", 53))
		err := k8s.Create(ctx, p)
		if err == nil {
			_ = k8s.Delete(ctx, p)
			t.Fatal("apiserver accepted a 53-character name; expected rejection")
		}
		if !strings.Contains(err.Error(), "52 characters or fewer") {
			t.Fatalf("error did not mention the name cap: %v", err)
		}
	})

	// The case that motivated the cap: past 63, the label selector itself is
	// invalid, so nothing the controller does can report the problem.
	t.Run("past the label cap rejected", func(t *testing.T) {
		p := defragPolicy(strings.Repeat("c", 64))
		err := k8s.Create(ctx, p)
		if err == nil {
			_ = k8s.Delete(ctx, p)
			t.Fatal("apiserver accepted a 64-character name; expected rejection")
		}
	})
}

// StartingDeadlineSeconds is multiplied out to a time.Duration, which overflows
// past ~292 years and wraps to a negative window that suppresses every tick.
func TestCEL_DefragPolicyStartingDeadlineBounded(t *testing.T) {
	skipIfNoEnvtest(t)
	ctx := context.Background()

	t.Run("ordinary deadline accepted", func(t *testing.T) {
		p := defragPolicy("deadline-ok")
		d := int64(3600)
		p.Spec.StartingDeadlineSeconds = &d
		if err := k8s.Create(ctx, p); err != nil {
			t.Fatalf("apiserver rejected a one-hour deadline: %v", err)
		}
		_ = k8s.Delete(ctx, p)
	})

	t.Run("overflowing deadline rejected", func(t *testing.T) {
		p := defragPolicy("deadline-overflow")
		d := int64(9223372037) // one second past what time.Duration can hold
		p.Spec.StartingDeadlineSeconds = &d
		err := k8s.Create(ctx, p)
		if err == nil {
			_ = k8s.Delete(ctx, p)
			t.Fatal("apiserver accepted an overflowing startingDeadlineSeconds")
		}
	})
}
