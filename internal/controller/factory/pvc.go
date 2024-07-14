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

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetPVCName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if len(cluster.Spec.Storage.VolumeClaimTemplate.Name) > 0 {
		return cluster.Spec.Storage.VolumeClaimTemplate.Name
	}
	//nolint:goconst
	return "data"
}

func PVCs(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, cli client.Client) ([]corev1.PersistentVolumeClaim, error) {
	labels := PVCLabels(cluster)
	pvcs := corev1.PersistentVolumeClaimList{}
	err := cli.List(ctx, &pvcs, client.MatchingLabels(labels))
	if err != nil {
		return nil, err
	}
	return pvcs.Items, nil
}
