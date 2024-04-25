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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func CreateOrUpdatePdb(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
) error {
	if cluster.Spec.PodDisruptionBudgetTemplate == nil {
		return deleteOwnedResource(ctx, rclient, &v1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}})
	}

	logger := log.FromContext(ctx)
	pdb := &v1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Spec: v1.PodDisruptionBudgetSpec{
			MinAvailable:   cluster.Spec.PodDisruptionBudgetTemplate.Spec.MinAvailable,
			MaxUnavailable: cluster.Spec.PodDisruptionBudgetTemplate.Spec.MaxUnavailable,
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

	logger.V(2).Info("pdb spec generated", "pdb_name", pdb.Name, "pdb_spec", pdb.Spec)

	if err := ctrl.SetControllerReference(cluster, pdb, rclient.Scheme()); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileOwnedResource(ctx, rclient, pdb)
}
