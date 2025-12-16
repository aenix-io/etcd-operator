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
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

var _ = Describe("Create services handlers", func() {
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
		DeferCleanup(k8sClient.Delete, ns)
	})

	Context("when creating cluster services", func() {
		var (
			etcdcluster     etcdaenixiov1alpha1.EtcdCluster
			headlessService corev1.Service
			clientService   corev1.Service

			err error
		)

		BeforeEach(func() {
			etcdcluster = etcdaenixiov1alpha1.EtcdCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-etcdcluster-",
					Namespace:    ns.GetName(),
					UID:          types.UID(uuid.NewString()),
				},
				Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(3)),
				},
			}
			Expect(k8sClient.Create(ctx, &etcdcluster)).Should(Succeed())
			Eventually(Get(&etcdcluster)).Should(Succeed())
			DeferCleanup(k8sClient.Delete, &etcdcluster)

			headlessService = corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.GetName(),
					Name:      GetHeadlessServiceName(&etcdcluster),
				},
			}
			clientService = corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.GetName(),
					Name:      GetServiceName(&etcdcluster),
				},
			}
		})

		AfterEach(func() {
			err = Get(&headlessService)()
			if err == nil {
				Expect(k8sClient.Delete(ctx, &headlessService)).Should(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
			err = Get(&clientService)()
			if err == nil {
				Expect(k8sClient.Delete(ctx, &clientService)).Should(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})

		It("should successfully create headless service object", func() {
			headlessSvcObj, err := GetHeadlessService(ctx, &etcdcluster, k8sClient)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(headlessSvcObj).ShouldNot(BeNil())
			Expect(headlessSvcObj).Should(SatisfyAll(
				HaveField("Spec.Type", Equal(corev1.ServiceTypeClusterIP)),
				HaveField("Spec.ClusterIP", Equal(corev1.ClusterIPNone)),
			))
		})

		It("should successfully create client service object", func() {
			clientSvcObj, err := GetClientService(ctx, &etcdcluster, k8sClient)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(clientSvcObj).ShouldNot(BeNil())
			Expect(clientSvcObj).Should(SatisfyAll(
				HaveField("Spec.Type", Equal(corev1.ServiceTypeClusterIP)),
				HaveField("Spec.ClusterIP", Equal(corev1.ClusterIPNone)),
			))
		})
	})
})
