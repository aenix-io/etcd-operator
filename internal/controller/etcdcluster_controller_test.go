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

package controller

import (
	"context"

	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller/factory"
)

var _ = Describe("EtcdCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		etcdcluster := &etcdaenixiov1alpha1.EtcdCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind EtcdCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, etcdcluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &etcdaenixiov1alpha1.EtcdCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
						Replicas: ptr.To(int32(3)),
						Storage: etcdaenixiov1alpha1.StorageSpec{
							EmptyDir: &v1.EmptyDirVolumeSource{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &etcdaenixiov1alpha1.EtcdCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance EtcdCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &EtcdClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, etcdcluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(etcdcluster.Status.Conditions).To(HaveLen(2))
			Expect(etcdcluster.Status.Conditions[0].Type).To(Equal(etcdaenixiov1alpha1.EtcdConditionInitialized))
			Expect(etcdcluster.Status.Conditions[0].Status).To(Equal(metav1.ConditionStatus("True")))
			Expect(etcdcluster.Status.Conditions[1].Type).To(Equal(etcdaenixiov1alpha1.EtcdConditionReady))
			Expect(etcdcluster.Status.Conditions[1].Status).To(Equal(metav1.ConditionStatus("False")))

			// check that ConfigMap is created
			cm := &v1.ConfigMap{}
			cmName := types.NamespacedName{
				Namespace: typeNamespacedName.Namespace,
				Name:      factory.GetClusterStateConfigMapName(etcdcluster),
			}
			err = k8sClient.Get(ctx, cmName, cm)
			Expect(err).NotTo(HaveOccurred(), "cluster configmap state should exist")
			Expect(cm.Data).To(HaveKeyWithValue("ETCD_INITIAL_CLUSTER_STATE", "new"))
			// check that Service is created
			svc := &v1.Service{}
			err = k8sClient.Get(ctx, typeNamespacedName, svc)
			Expect(err).NotTo(HaveOccurred(), "cluster headless Service should exist")
			Expect(svc.Spec.ClusterIP).To(Equal("None"), "cluster Service should be headless")
			// check that StatefulSet is created
			sts := &appsv1.StatefulSet{}
			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred(), "cluster statefulset should exist")
			// check that Service is created
			svc = &v1.Service{}
			clientSvcName := types.NamespacedName{
				Namespace: typeNamespacedName.Namespace,
				Name:      controllerReconciler.getClientServiceName(etcdcluster),
			}
			err = k8sClient.Get(ctx, clientSvcName, svc)
			Expect(err).NotTo(HaveOccurred(), "cluster client Service should exist")
			Expect(svc.Spec.ClusterIP).NotTo(Equal("None"), "cluster client Service should NOT be headless")
		})

		It("should successfully reconcile the resource twice and mark as ready", func() {
			By("Reconciling the created resource twice (second time after marking sts as ready)")
			controllerReconciler := &EtcdClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// check that StatefulSet is created
			sts := &appsv1.StatefulSet{}
			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred(), "cluster statefulset should exist")
			// mark sts as ready
			sts.Status.ReadyReplicas = *etcdcluster.Spec.Replicas
			sts.Status.Replicas = *etcdcluster.Spec.Replicas
			Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
			// reconcile and check EtcdCluster status
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// check EtcdCluster status
			err = k8sClient.Get(ctx, typeNamespacedName, etcdcluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(etcdcluster.Status.Conditions[1].Type).To(Equal(etcdaenixiov1alpha1.EtcdConditionReady))
			Expect(string(etcdcluster.Status.Conditions[1].Status)).To(Equal("True"))
		})
	})
})
