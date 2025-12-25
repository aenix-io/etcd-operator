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
	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("UpdatePersistentVolumeClaims", func() {
	var (
		cluster *etcdaenixiov1alpha1.EtcdCluster
	)

	BeforeEach(func() {
		cluster = &etcdaenixiov1alpha1.EtcdCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
				Storage: etcdaenixiov1alpha1.StorageSpec{
					VolumeClaimTemplate: etcdaenixiov1alpha1.EmbeddedPersistentVolumeClaim{
						EmbeddedObjectMetadata: etcdaenixiov1alpha1.EmbeddedObjectMetadata{
							Name: "custom-data",
							Labels: map[string]string{
								"app":        "etcd",
								"custom-key": "custom-value",
							},
						},
					},
				},
			},
		}
	})

	Context("PVCLabels function", func() {
		When("cluster has storage with labels", func() {
			It("should return combined labels from PodLabels and VolumeClaimTemplate", func() {
				labels := PVCLabels(cluster)

				Expect(labels).To(HaveKeyWithValue("app", "etcd"))
				Expect(labels).To(HaveKeyWithValue("custom-key", "custom-value"))
				Expect(labels).To(HaveKey("app.kubernetes.io/name"))
				Expect(labels).To(HaveKey("app.kubernetes.io/instance"))
				Expect(labels).To(HaveKey("app.kubernetes.io/managed-by"))
			})

			When("VolumeClaimTemplate labels override PodLabels", func() {
				BeforeEach(func() {
					cluster.Spec.Storage.VolumeClaimTemplate.Labels["app.kubernetes.io/name"] = "overridden-etcd"
				})

				It("should prioritize VolumeClaimTemplate labels", func() {
					labels := PVCLabels(cluster)

					Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/name", "overridden-etcd"))
					Expect(labels).To(HaveKeyWithValue("custom-key", "custom-value"))
				})
			})
		})

		When("VolumeClaimTemplate has no labels", func() {
			BeforeEach(func() {
				cluster.Spec.Storage.VolumeClaimTemplate.Labels = nil
			})

			It("should return only PodLabels", func() {
				labels := PVCLabels(cluster)

				Expect(labels).To(HaveKey("app.kubernetes.io/name"))
				Expect(labels).To(HaveKey("app.kubernetes.io/instance"))
				Expect(labels).NotTo(HaveKey("custom-key"))
			})
		})

		When("VolumeClaimTemplate is nil", func() {
			BeforeEach(func() {
				cluster.Spec.Storage.VolumeClaimTemplate = etcdaenixiov1alpha1.EmbeddedPersistentVolumeClaim{}
			})

			It("should return only PodLabels", func() {
				labels := PVCLabels(cluster)

				Expect(labels).To(HaveKey("app.kubernetes.io/name"))
				Expect(labels).To(HaveKey("app.kubernetes.io/instance"))
				Expect(labels).NotTo(HaveKey("custom-key"))
			})
		})
	})

	Context("GetPVCName function", func() {
		When("VolumeClaimTemplate has a name", func() {
			It("should return the custom name", func() {
				name := GetPVCName(cluster)
				Expect(name).To(Equal("custom-data"))
			})
		})

		When("VolumeClaimTemplate name is empty", func() {
			BeforeEach(func() {
				cluster.Spec.Storage.VolumeClaimTemplate.Name = ""
			})

			It("should return default name 'data'", func() {
				name := GetPVCName(cluster)
				Expect(name).To(Equal("data"))
			})
		})

		When("VolumeClaimTemplate name is whitespace only", func() {
			BeforeEach(func() {
				cluster.Spec.Storage.VolumeClaimTemplate.Name = "   "
			})

			It("should return default name 'data'", func() {
				name := GetPVCName(cluster)
				Expect(name).To(Equal("   "))
			})
		})

		When("VolumeClaimTemplate is nil", func() {
			BeforeEach(func() {
				cluster.Spec.Storage.VolumeClaimTemplate = etcdaenixiov1alpha1.EmbeddedPersistentVolumeClaim{}
			})

			It("should return default name 'data'", func() {
				name := GetPVCName(cluster)
				Expect(name).To(Equal("data"))
			})
		})
	})
})
