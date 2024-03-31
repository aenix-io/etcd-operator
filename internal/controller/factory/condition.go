/*
Copyright 2024 The etcd-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package factory

import (
	"slices"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition is a wrapper over metav1.Condition type.
type Condition struct {
	metav1.Condition
}

// NewCondition returns Condition object with provided condition type.
func NewCondition(conditionType etcdaenixiov1alpha1.EtcdCondType) Condition {
	return Condition{metav1.Condition{Type: string(conditionType)}}
}

// WithStatus converts boolean value passed as an argument to metav1.ConditionStatus type and sets condition status.
func (c Condition) WithStatus(status bool) Condition {
	c.Status = metav1.ConditionFalse
	if status {
		c.Status = metav1.ConditionTrue
	}
	return c
}

// WithReason sets condition reason.
func (c Condition) WithReason(reason string) Condition {
	c.Reason = reason
	return c
}

// WithMessage sets condition message.
func (c Condition) WithMessage(message string) Condition {
	c.Message = message
	return c
}

// Complete finalizes condition building by setting transition timestamp and returns wrapped metav1.Condition object.
func (c Condition) Complete() metav1.Condition {
	c.LastTransitionTime = metav1.Now()
	return c.Condition
}

// FillConditions fills EtcdCluster .status.Conditions list with all available conditions in "False" status.
func FillConditions(cluster *etcdaenixiov1alpha1.EtcdCluster) {
	SetCondition(cluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionInitialized).
		WithStatus(false).
		WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted)).
		WithMessage(string(etcdaenixiov1alpha1.EtcdInitCondNegMessage)).
		Complete())
	SetCondition(cluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionReady).
		WithStatus(false).
		WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeWaitingForFirstQuorum)).
		WithMessage(string(etcdaenixiov1alpha1.EtcdReadyCondNegWaitingForQuorum)).
		Complete())
}

// SetCondition sets either replaces corresponding existing condition in the .status.Conditions list or appends
// one passed as an argument. In case operation will not result into condition status change, return.
func SetCondition(
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	condition metav1.Condition,
) {
	condition.ObservedGeneration = cluster.GetGeneration()
	idx := slices.IndexFunc(cluster.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == condition.Type
	})

	if idx == -1 {
		cluster.Status.Conditions = append(cluster.Status.Conditions, condition)
		return
	}
	statusNotChanged := cluster.Status.Conditions[idx].Status == condition.Status
	reasonNotChanged := cluster.Status.Conditions[idx].Reason == condition.Reason
	if statusNotChanged && reasonNotChanged {
		return
	}
	cluster.Status.Conditions[idx] = condition
}

// GetCondition returns condition from cluster status conditions by type or nil if not present.
func GetCondition(cluster *etcdaenixiov1alpha1.EtcdCluster, condType string) *metav1.Condition {
	idx := slices.IndexFunc(cluster.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == condType
	})
	if idx == -1 {
		return nil
	}

	return &cluster.Status.Conditions[idx]
}
