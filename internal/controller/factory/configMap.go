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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

func GetClusterStateConfigMapName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	return cluster.Name + "-cluster-state"
}

func CreateOrUpdateClusterStateConfigMap(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	initialCluster := ""
	for i := int32(0); i < *cluster.Spec.Replicas; i++ {
		if i > 0 {
			initialCluster += ","
		}
		initialCluster += fmt.Sprintf("%s-%d=https://%s-%d.%s.%s.svc:2380",
			cluster.Name, i,
			cluster.Name, i, cluster.Name, cluster.Namespace,
		)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      GetClusterStateConfigMapName(cluster),
		},
		Data: map[string]string{
			"ETCD_INITIAL_CLUSTER_STATE": "new",
			"ETCD_INITIAL_CLUSTER":       initialCluster,
			"ETCD_INITIAL_CLUSTER_TOKEN": cluster.Name + "-" + cluster.Namespace,
		},
	}

	if isEtcdClusterReady(cluster) {
		// update cluster state to existing
		configMap.Data["ETCD_INITIAL_CLUSTER_STATE"] = "existing"
	}

	if err := ctrl.SetControllerReference(cluster, configMap, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileConfigMap(ctx, rclient, cluster.Name, configMap)
}

// isEtcdClusterReady returns true if condition "Ready" has progressed
// from reason v1alpha1.EtcdCondTypeWaitingForFirstQuorum.
func isEtcdClusterReady(cluster *etcdaenixiov1alpha1.EtcdCluster) bool {
	cond := GetCondition(cluster, etcdaenixiov1alpha1.EtcdConditionReady)
	return cond != nil && (cond.Reason == string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady) ||
		cond.Reason == string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetNotReady))
}
