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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var _ = Describe("CreateOrUpdatePdb handlers", func() {
	Context("Should successfully create PDB resource for cluster", func() {
		const resourceName = "test-resource"
		ctx := context.Background()
		etcdcluster := &etcdaenixiov1alpha1.EtcdCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
				UID:       "test-uid",
			},
			Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
				Replicas:            ptr.To(int32(3)),
				PodDisruptionBudget: &etcdaenixiov1alpha1.EmbeddedPodDisruptionBudget{},
			},
		}
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}
		AfterEach(func() {
			By("deleting pdb if it exists", func() {
				pdb := &v1.PodDisruptionBudget{}
				err := k8sClient.Get(ctx, typeNamespacedName, pdb)
				if err == nil {
					Expect(k8sClient.Delete(ctx, pdb)).To(Succeed())
				} else {
					Expect(errors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound error, got %#v", err))
				}
			})
		})

		It("should create PDB with pre-filled data", func() {
			cluster := etcdcluster.DeepCopy()
			cluster.Spec.PodDisruptionBudget.Spec.MinAvailable = ptr.To(intstr.FromInt32(int32(3)))
			err := CreateOrUpdatePdb(ctx, cluster, k8sClient, k8sClient.Scheme())
			Expect(err).To(Succeed())

			pdb := &v1.PodDisruptionBudget{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, pdb)).To(Succeed(), "PDB should exist after reconciliation")
			if Expect(pdb.Spec.MinAvailable).NotTo(BeNil()) {
				Expect(pdb.Spec.MinAvailable.IntValue()).To(Equal(3))
			}
			Expect(pdb.Spec.MaxUnavailable).To(BeNil())
		})
		It("Should create PDB with empty data", func() {
			cluster := etcdcluster.DeepCopy()
			err := CreateOrUpdatePdb(ctx, cluster, k8sClient, k8sClient.Scheme())
			Expect(err).To(Succeed())

			pdb := &v1.PodDisruptionBudget{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, pdb)).To(Succeed())
			if Expect(pdb.Spec.MinAvailable).NotTo(BeNil()) {
				Expect(cluster.Spec.PodDisruptionBudget.Spec.MinAvailable).To(BeNil())
				Expect(pdb.Spec.MinAvailable.IntValue()).To(Equal(2))
			}
			Expect(pdb.Spec.MaxUnavailable).To(BeNil())
		})

		It("Should skip deletion of PDB if not filled and not exist", func() {
			cluster := etcdcluster.DeepCopy()
			cluster.Spec.PodDisruptionBudget = nil
			err := CreateOrUpdatePdb(ctx, cluster, k8sClient, k8sClient.Scheme())
			Expect(err).To(Succeed())
		})

		It("should delete created PDB after updating CR", func() {
			cluster := etcdcluster.DeepCopy()
			err := CreateOrUpdatePdb(ctx, cluster, k8sClient, k8sClient.Scheme())
			Expect(err).To(Succeed())
			pdb := &v1.PodDisruptionBudget{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, pdb)).To(Succeed())

			cluster.Spec.PodDisruptionBudget = nil
			err = CreateOrUpdatePdb(ctx, cluster, k8sClient, k8sClient.Scheme())
			Expect(err).To(Succeed())
			err = k8sClient.Get(ctx, typeNamespacedName, pdb)
			Expect(errors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound error, got %#v", err))
		})
	})
})
