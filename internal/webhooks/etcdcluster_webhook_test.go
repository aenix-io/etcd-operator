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

package webhooks

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
)

var _ = Describe("EtcdCluster Webhook", func() {

	Context("When creating EtcdCluster under Defaulting Webhook", func() {
		It("Should fill in the default value if a required field is empty", func() {
			etcdCluster := &v1alpha1.EtcdCluster{}
			err := (&EtcdCluster{}).Default(context.Background(), etcdCluster)
			gomega.Expect(err).To(gomega.Succeed())
			gomega.Expect(etcdCluster.Spec.PodSpec.Image).To(gomega.Equal(v1alpha1.DefaultEtcdImage))
			gomega.Expect(etcdCluster.Spec.Replicas).To(gomega.BeNil(), "User should have an opportunity to create cluster with 0 replicas")
			gomega.Expect(etcdCluster.Spec.Storage.EmptyDir).To(gomega.BeNil())
			storage := etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()
			if gomega.Expect(storage).NotTo(gomega.BeNil()) {
				gomega.Expect(*storage).To(gomega.Equal(resource.MustParse("4Gi")))
			}
		})

		It("Should not override fields with default values if not empty", func() {
			etcdCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(5)),
					PodSpec: v1alpha1.PodSpec{
						Image: "myregistry.local/etcd:v1.1.1",
					},
					Storage: v1alpha1.StorageSpec{
						VolumeClaimTemplate: v1alpha1.EmbeddedPersistentVolumeClaim{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								StorageClassName: ptr.To("local-path"),
								Resources: corev1.VolumeResourceRequirements{
									Requests: map[corev1.ResourceName]resource.Quantity{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			}
			err := (&EtcdCluster{}).Default(context.Background(), etcdCluster)
			gomega.Expect(err).To(gomega.Succeed())
			gomega.Expect(*etcdCluster.Spec.Replicas).To(gomega.Equal(int32(5)))
			gomega.Expect(etcdCluster.Spec.PodSpec.Image).To(gomega.Equal("myregistry.local/etcd:v1.1.1"))
			gomega.Expect(etcdCluster.Spec.Storage.EmptyDir).To(gomega.BeNil())
			storage := etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()
			if gomega.Expect(storage).NotTo(gomega.BeNil()) {
				gomega.Expect(*storage).To(gomega.Equal(resource.MustParse("10Gi")))
			}
		})
	})

	Context("When creating EtcdCluster under Validating Webhook", func() {
		It("Should admit if all required fields are provided", func() {
			etcdCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(1)),
				},
			}
			w, err := (&EtcdCluster{}).ValidateCreate(context.Background(), etcdCluster)
			gomega.Expect(err).To(gomega.Succeed())
			gomega.Expect(w).To(gomega.BeEmpty())
		})
	})

	Context("When updating EtcdCluster under Validating Webhook", func() {
		It("Should reject changing storage type", func() {
			etcdCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(1)),
					Storage:  v1alpha1.StorageSpec{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			}
			oldCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(1)),
					Storage:  v1alpha1.StorageSpec{EmptyDir: nil},
				},
			}
			_, err := (&EtcdCluster{}).ValidateUpdate(context.Background(), oldCluster, etcdCluster)
			if gomega.Expect(err).To(gomega.HaveOccurred()) {
				statusErr := err.(*errors.StatusError)
				gomega.Expect(statusErr.ErrStatus.Message).To(gomega.ContainSubstring("field is immutable"))
			}
		})

		It("Should allow changing emptydir size", func() {
			etcdCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(1)),
					Storage:  v1alpha1.StorageSpec{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("4Gi"))}},
				},
			}
			oldCluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Replicas: ptr.To(int32(1)),
					Storage:  v1alpha1.StorageSpec{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("10Gi"))}},
				},
			}
			_, err := (&EtcdCluster{}).ValidateUpdate(context.Background(), oldCluster, etcdCluster)
			gomega.Expect(err).To(gomega.Succeed())
		})
	})
})
