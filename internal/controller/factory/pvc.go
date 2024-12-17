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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func PVCLabels(cluster *etcdaenixiov1alpha1.EtcdCluster) map[string]string {
	labels := PodLabels(cluster)
	for key, value := range cluster.Spec.Storage.VolumeClaimTemplate.Labels {
		labels[key] = value
	}
	return labels
}

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

// UpdatePersistentVolumeClaims checks and updates the sizes of PVCs in an EtcdCluster if the specified storage size is larger than the current.
func UpdatePersistentVolumeClaims(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, rclient client.Client) error {
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": cluster.Name,
	})
	listOptions := &client.ListOptions{
		Namespace:     cluster.Namespace,
		LabelSelector: labelSelector,
	}

	// List all PVCs in the same namespace as the cluster using the label selector
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := rclient.List(ctx, pvcList, listOptions)
	if err != nil {
		return fmt.Errorf("failed to list PVCs: %w", err)
	}

	// Desired size from the cluster spec
	expectedPrefix := fmt.Sprintf("data-%s-", cluster.Name)
	desiredSize := cluster.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]

	for _, pvc := range pvcList.Items {
		// Skip if PVC name does not match expected prefix
		if !strings.HasPrefix(pvc.Name, expectedPrefix) {
			continue
		}

		// Skip if specified StorageClass does not support volume expansion
		if pvc.Spec.StorageClassName != nil {
			sc := &storagev1.StorageClass{}
			scName := *pvc.Spec.StorageClassName
			err := rclient.Get(ctx, types.NamespacedName{Name: scName}, sc)
			if err != nil {
				return fmt.Errorf("failed to get StorageClass '%s' for PVC '%s': %w", scName, pvc.Name, err)
			}
			if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
				continue
			}
		}

		currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		// Only patch if the desired size is greater than the current size
		if desiredSize.Cmp(currentSize) == 1 {
			newSizePatch := []byte(fmt.Sprintf(`{"spec": {"resources": {"requests": {"storage": "%s"}}}}`, desiredSize.String()))
			err = rclient.Patch(ctx, &pvc, client.RawPatch(types.StrategicMergePatchType, newSizePatch))
			if err != nil {
				return fmt.Errorf("failed to patch PVC %s for updated size: %w", pvc.Name, err)
			}
		}
	}

	return nil
}
