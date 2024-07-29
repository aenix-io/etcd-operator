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

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetClusterStateConfigMapName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	return cluster.Name + "-cluster-state"
}

func CreateOrUpdateClusterStateConfigMap(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
) error {
	var err error
	initialCluster := ""
	clusterService := fmt.Sprintf("%s.%s.svc:2380", GetHeadlessServiceName(cluster), cluster.Namespace)
	for i := int32(0); i < *cluster.Spec.Replicas; i++ {
		if i > 0 {
			initialCluster += ","
		}
		podName := fmt.Sprintf("%s-%d", cluster.Name, i)
		initialCluster += fmt.Sprintf("%s=https://%s.%s",
			podName, podName, clusterService,
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
	ctx, err = contextWithGVK(ctx, configMap, rclient.Scheme())
	if err != nil {
		return err
	}

	if isEtcdClusterReady(cluster) {
		// update cluster state to existing
		log.Debug(ctx, "updating cluster state")
		configMap.Data["ETCD_INITIAL_CLUSTER_STATE"] = "existing"
	}
	log.Debug(ctx, "configmap data generated", "data", configMap.Data)

	if err := ctrl.SetControllerReference(cluster, configMap, rclient.Scheme()); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileOwnedResource(ctx, rclient, configMap)
}

// isEtcdClusterReady returns true if condition "Ready" has progressed
// from reason v1alpha1.EtcdCondTypeWaitingForFirstQuorum.
func isEtcdClusterReady(cluster *etcdaenixiov1alpha1.EtcdCluster) bool {
	cond := meta.FindStatusCondition(cluster.Status.Conditions, etcdaenixiov1alpha1.EtcdConditionReady)
	return cond != nil && (cond.Reason == string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady) ||
		cond.Reason == string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetNotReady))
}
