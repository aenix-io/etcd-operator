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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

var _ = Describe("CreateOrUpdateClusterStateConfigMap handlers", func() {
	Context("When ensuring a configMap", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		etcdcluster := &etcdaenixiov1alpha1.EtcdCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
				UID:       "test-uid",
			},
			Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
				Replicas: ptr.To(int32(3)),
			},
		}
		typeNamespacedName := types.NamespacedName{
			Name:      GetClusterStateConfigMapName(etcdcluster),
			Namespace: "default",
		}

		It("should successfully ensure the configmap", func() {
			cm := &corev1.ConfigMap{}
			var err error
			var cmUid types.UID
			By("creating the configmap for initial cluster", func() {
				err = CreateOrUpdateClusterStateConfigMap(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Get(ctx, typeNamespacedName, cm)
				cmUid = cm.UID
				Expect(err).NotTo(HaveOccurred())
				Expect(cm.Data["ETCD_INITIAL_CLUSTER_STATE"]).To(Equal("new"))
			})

			By("updating the configmap for initialized cluster", func() {
				SetCondition(etcdcluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionReady).
					WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady)).
					WithStatus(true).
					Complete())
				err = CreateOrUpdateClusterStateConfigMap(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Get(ctx, typeNamespacedName, cm)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm.Data["ETCD_INITIAL_CLUSTER_STATE"]).To(Equal("existing"))
				// Check that we are updating the same configmap
				Expect(cm.UID).To(Equal(cmUid))
			})

			By("updating the configmap back to new", func() {
				SetCondition(etcdcluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionReady).
					WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeWaitingForFirstQuorum)).
					WithStatus(true).
					Complete())
				err = CreateOrUpdateClusterStateConfigMap(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Get(ctx, typeNamespacedName, cm)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm.Data["ETCD_INITIAL_CLUSTER_STATE"]).To(Equal("new"))
				// Check that we are updating the same configmap
				Expect(cm.UID).To(Equal(cmUid))
			})

			By("deleting the configmap", func() {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			})
		})

		It("should fail on creating the configMap with invalid owner reference", func() {
			etcdcluster := etcdcluster.DeepCopy()
			emptyScheme := runtime.NewScheme()

			err := CreateOrUpdateClusterStateConfigMap(ctx, etcdcluster, k8sClient, emptyScheme)
			Expect(err).To(HaveOccurred())
		})
	})
})
