package util

import (
	"slices"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EtcdClusterIsReady returns true is condition "Ready" has status equals to "True", otherwise false.
func EtcdClusterIsReady(cluster *etcdaenixiov1alpha1.EtcdCluster) bool {
	idx := slices.IndexFunc(cluster.Status.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == etcdaenixiov1alpha1.EtcdConditionReady
	})
	if idx == -1 {
		return false
	}
	return cluster.Status.Conditions[idx].Status == metav1.ConditionTrue
}

// FillConditions fills EtcdCluster .status.Conditions list with all available conditions in "False" status.
func FillConditions(cluster *etcdaenixiov1alpha1.EtcdCluster) {
	conditions := []string{
		etcdaenixiov1alpha1.EtcdConditionInitialized,
		etcdaenixiov1alpha1.EtcdConditionReady,
	}

	for _, c := range conditions {
		SetCondition(cluster, c, false)
	}
}

// SetCondition sets the condition of defined type to desired state.
func SetCondition(
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	conditionType string,
	conditionStatus bool,
) {
	status := metav1.ConditionFalse
	if conditionStatus {
		status = metav1.ConditionTrue
	}

	idx := slices.IndexFunc(cluster.Status.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == conditionType
	})

	reason, message := conditionReasonMessage(conditionType, status)
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	if idx == -1 {
		cluster.Status.Conditions = append(cluster.Status.Conditions, condition)
		return
	}
	if cluster.Status.Conditions[idx].Status == status {
		return
	}
	cluster.Status.Conditions[idx] = condition
}

func conditionReasonMessage(conditionType string, status metav1.ConditionStatus) (string, string) {
	var (
		reason, message string
	)

	switch {
	case conditionType == etcdaenixiov1alpha1.EtcdConditionInitialized && status == metav1.ConditionFalse:
		reason = string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted)
		message = string(etcdaenixiov1alpha1.EtcdInitCondNegMessage)
	case conditionType == etcdaenixiov1alpha1.EtcdConditionInitialized && status == metav1.ConditionTrue:
		reason = string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete)
		message = string(etcdaenixiov1alpha1.EtcdInitCondPosMessage)
	case conditionType == etcdaenixiov1alpha1.EtcdConditionReady && status == metav1.ConditionFalse:
		reason = string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetNotReady)
		message = string(etcdaenixiov1alpha1.EtcdReadyCondNegMessage)
	case conditionType == etcdaenixiov1alpha1.EtcdConditionReady && status == metav1.ConditionTrue:
		reason = string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady)
		message = string(etcdaenixiov1alpha1.EtcdReadyCondPosMessage)
	}
	return reason, message
}
