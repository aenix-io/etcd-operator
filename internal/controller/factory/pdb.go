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
	v1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateOrUpdatePdb(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	if cluster.Spec.PodDisruptionBudget == nil {
		return deleteManagedPdb(ctx, rclient, &v1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}})
	}

	pdb := &v1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Spec: v1.PodDisruptionBudgetSpec{
			MinAvailable:   cluster.Spec.PodDisruptionBudget.Spec.MinAvailable,
			MaxUnavailable: cluster.Spec.PodDisruptionBudget.Spec.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
			},
			UnhealthyPodEvictionPolicy: ptr.To(v1.IfHealthyBudget),
		},
	}
	// if both are nil, calculate minAvailable based on number of replicas in the cluster
	if pdb.Spec.MinAvailable == nil && pdb.Spec.MaxUnavailable == nil {
		pdb.Spec.MinAvailable = ptr.To(intstr.FromInt32(int32(cluster.CalculateQuorumSize())))
	}

	if err := ctrl.SetControllerReference(cluster, pdb, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcilePdb(ctx, rclient, cluster.Name, pdb)
}
