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
	"k8s.io/apimachinery/pkg/util/intstr"
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
			Expect(sts.Spec.Template.Spec.Containers[0].Command).To(Equal(generateEtcdCommand()))

			By("Checking the default startup probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].StartupProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/readyz?serializable=false",
							Port:   intstr.FromInt32(2379),
							Scheme: v1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    5,
					SuccessThreshold: 1,
					FailureThreshold: 3,
				}))
			})

			By("Checking the default readiness probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].ReadinessProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/readyz",
							Port:   intstr.FromInt32(2379),
							Scheme: v1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    5,
					SuccessThreshold: 1,
					FailureThreshold: 3,
				}))
			})

			By("Checking the default liveness probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].LivenessProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/livez",
							Port:   intstr.FromInt32(2379),
							Scheme: v1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    5,
					SuccessThreshold: 1,
					FailureThreshold: 3,
				}))
			})

			By("Deleting the statefulset", func() {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			})
		})

		It("should successfully override probes", func() {
			etcdcluster := etcdcluster.DeepCopy()
			etcdcluster.Spec.PodSpec = etcdaenixiov1alpha1.PodSpec{
				LivenessProbe: &v1.Probe{
					InitialDelaySeconds: 13,
					PeriodSeconds:       11,
				},
				ReadinessProbe: &v1.Probe{
					PeriodSeconds: 3,
				},
				StartupProbe: &v1.Probe{
					PeriodSeconds: 7,
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path: "/test",
							Port: intstr.FromInt32(2389),
						},
					},
				},
			}

			sts := &appsv1.StatefulSet{}
			err := CreateOrUpdateStatefulSet(ctx, etcdcluster, k8sClient, k8sClient.Scheme())
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Checking the updated startup probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].StartupProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/test",
							Port:   intstr.FromInt32(2389),
							Scheme: v1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    7,
					SuccessThreshold: 1,
					FailureThreshold: 3,
				}))
			})

			By("Checking the updated readiness probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].ReadinessProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/readyz",
							Port:   intstr.FromInt32(2379),
							Scheme: v1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    3,
					SuccessThreshold: 1,
					FailureThreshold: 3,
				}))
			})

			By("Checking the updated liveness probe", func() {
				Expect(sts.Spec.Template.Spec.Containers[0].LivenessProbe).To(Equal(&v1.Probe{
					ProbeHandler: v1.ProbeHandler{
						HTTPGet: &v1.HTTPGetAction{
							Path:   "/livez",
							Port:   intstr.FromInt32(2379),
							Scheme: v1.URISchemeHTTP,
						},
					},
					InitialDelaySeconds: 13,
					TimeoutSeconds:      1,
					PeriodSeconds:       11,
					SuccessThreshold:    1,
					FailureThreshold:    3,
				}))
			})

			By("Deleting the statefulset", func() {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			})
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

	Context("When getting liveness probe", func() {
		It("should correctly get default values", func() {
			probe := getLivenessProbe(nil)
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/livez",
						Port: intstr.FromInt32(2379),
					},
				},
				PeriodSeconds: 5,
			}))
		})
		It("should correctly override all values", func() {
			probe := getLivenessProbe(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/liveznew",
						Port: intstr.FromInt32(2390),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/liveznew",
						Port: intstr.FromInt32(2390),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			}))
		})
		It("should correctly override partial changes ", func() {
			probe := getLivenessProbe(&v1.Probe{
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/livez",
						Port: intstr.FromInt32(2379),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			}))
		})
	})

	Context("When getting startup probe", func() {
		It("should correctly get default values", func() {
			probe := getStartupProbe(nil)
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/readyz?serializable=false",
						Port: intstr.FromInt32(2379),
					},
				},
				PeriodSeconds: 5,
			}))
		})
		It("should correctly override all values", func() {
			probe := getStartupProbe(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.FromInt32(2390),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.FromInt32(2390),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			}))
		})
		It("should correctly override partial changes", func() {
			probe := getStartupProbe(&v1.Probe{
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/readyz?serializable=false",
						Port: intstr.FromInt32(2379),
					},
				},
				InitialDelaySeconds: 7,
				PeriodSeconds:       3,
			}))
		})
	})

	Context("When getting liveness probe", func() {
		It("should correctly get default values", func() {
			probe := getLivenessProbe(nil)
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/livez",
						Port: intstr.FromInt32(2379),
					},
				},
				PeriodSeconds: 5,
			}))
		})
		It("should correctly override all values", func() {
			probe := getLivenessProbe(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/liveznew",
						Port: intstr.FromInt32(2371),
					},
				},
				InitialDelaySeconds: 11,
				PeriodSeconds:       13,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/liveznew",
						Port: intstr.FromInt32(2371),
					},
				},
				InitialDelaySeconds: 11,
				PeriodSeconds:       13,
			}))
		})
		It("should correctly override partial changes", func() {
			probe := getLivenessProbe(&v1.Probe{
				InitialDelaySeconds: 11,
				PeriodSeconds:       13,
			})
			Expect(probe).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/livez",
						Port: intstr.FromInt32(2379),
					},
				},
				InitialDelaySeconds: 11,
				PeriodSeconds:       13,
			}))
		})
	})
	Context("When merge with default probe", func() {
		It("should correctly merge probe with default", func() {
			defaultProbe := v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/livez",
						Port: intstr.FromInt32(2379),
					},
				},
				PeriodSeconds: 5,
			}
			defaultProbeCopy := defaultProbe.DeepCopy()

			probe := &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					Exec: &v1.ExecAction{
						Command: []string{"test"},
					},
				},
				InitialDelaySeconds: 11,
			}
			result := mergeWithDefaultProbe(probe, defaultProbe)
			Expect(result).To(Equal(&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					Exec: &v1.ExecAction{
						Command: []string{"test"},
					},
				},
				InitialDelaySeconds: 11,
				PeriodSeconds:       5,
			}))
			By("Shouldn't mutate default probe", func() {
				Expect(defaultProbe).To(Equal(*defaultProbeCopy))
			})
		})
	})
})
