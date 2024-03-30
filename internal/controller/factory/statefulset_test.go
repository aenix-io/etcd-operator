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

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

var _ = Describe("CreateOrUpdateStatefulSet handler", func() {
	Context("When ensuring a statefulset", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
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

		It("should successfully create the statefulset with empty spec", func() {
			sts := &appsv1.StatefulSet{}
			err := CreateOrUpdateStatefulSet(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred())
			Expect(sts.Spec.Replicas).To(Equal(etcdcluster.Spec.Replicas))

			Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
		})

		It("should successfully create the statefulset with filled spec", func() {
			By("Creating the statefulset")
			etcdcluster := etcdcluster.DeepCopy()
			etcdcluster.Spec.Storage = etcdaenixiov1alpha1.StorageSpec{
				VolumeClaimTemplate: etcdaenixiov1alpha1.EmbeddedPersistentVolumeClaim{
					EmbeddedObjectMetadata: etcdaenixiov1alpha1.EmbeddedObjectMetadata{
						Name: "etcd-data",
					},
					Spec: v1.PersistentVolumeClaimSpec{
						AccessModes: []v1.PersistentVolumeAccessMode{
							v1.ReadWriteOnce,
						},
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
					Status: v1.PersistentVolumeClaimStatus{},
				},
			}
			etcdcluster.Spec.PodSpec = etcdaenixiov1alpha1.PodSpec{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("100m"),
						v1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				PodMetadata: &etcdaenixiov1alpha1.EmbeddedObjectMetadata{
					Name: "test-pod",
					Labels: map[string]string{
						"app": "etcd",
					},
					Annotations: map[string]string{
						"app": "etcd",
					},
				},
			}

			sts := &appsv1.StatefulSet{}
			err := CreateOrUpdateStatefulSet(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Checking the resources")
			Expect(sts.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu()).To(Equal(etcdcluster.Spec.PodSpec.Resources.Requests.Cpu()))
			Expect(sts.Spec.Template.Spec.Containers[0].Resources.Requests.Memory()).To(Equal(etcdcluster.Spec.PodSpec.Resources.Requests.Memory()))

			By("Checking the pod metadata")
			Expect(sts.Spec.Template.ObjectMeta.GenerateName).To(Equal(etcdcluster.Spec.PodSpec.PodMetadata.Name))
			Expect(sts.Spec.Template.ObjectMeta.Labels).To(Equal(map[string]string{
				"app.kubernetes.io/name":       "etcd",
				"app.kubernetes.io/instance":   etcdcluster.Name,
				"app.kubernetes.io/managed-by": "etcd-operator",
				"app":                          "etcd",
			}))
			Expect(sts.Spec.Template.ObjectMeta.Annotations).To(Equal(etcdcluster.Spec.PodSpec.PodMetadata.Annotations))

			By("Checking the extraArgs")
			Expect(sts.Spec.Template.Spec.Containers[0].Command).To(Equal(generateEtcdCommand(etcdcluster)))

			By("Deleting the statefulset")
			Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
		})

		It("should successfully create the statefulset with emptyDir", func() {
			By("Creating the statefulset")
			etcdcluster := etcdcluster.DeepCopy()
			size := resource.MustParse("1Gi")
			etcdcluster.Spec.Storage = etcdaenixiov1alpha1.StorageSpec{
				EmptyDir: &v1.EmptyDirVolumeSource{
					SizeLimit: &size,
				},
			}

			sts := &appsv1.StatefulSet{}
			err := CreateOrUpdateStatefulSet(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Checking the emptyDir")
			Expect(sts.Spec.Template.Spec.Volumes[0].VolumeSource.EmptyDir.SizeLimit.String()).To(Equal(size.String()))

			By("Deleting the statefulset")
			Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
		})

		It("should fail on creating the statefulset with invalid owner reference", func() {
			etcdcluster := etcdcluster.DeepCopy()
			emptyScheme := runtime.NewScheme()

			err := CreateOrUpdateStatefulSet(ctx, etcdcluster, k8sClient, emptyScheme)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When generating a etcd command", func() {
		It("should correctly fillExtraArgs", func() {
			extraArgs := map[string]string{
				"key1": "value1",
				"key2": "value2",
			}
			etcdcluster := &etcdaenixiov1alpha1.EtcdCluster{
				Spec: etcdaenixiov1alpha1.EtcdClusterSpec{
					PodSpec: etcdaenixiov1alpha1.PodSpec{
						ExtraArgs: extraArgs,
					},
				},
			}

			args := generateEtcdArgs(etcdcluster)

			Expect(args).To(ContainElements([]string{
				"--key1=value1",
				"--key2=value2",
			}))
		})
	})
})
