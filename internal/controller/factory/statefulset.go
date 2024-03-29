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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

func CreateOrUpdateStatefulSet(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	podMetadata := metav1.ObjectMeta{
		Labels: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
	}

	if cluster.Spec.PodSpec.PodMetadata != nil {
		if cluster.Spec.PodSpec.PodMetadata.Name != "" {
			podMetadata.GenerateName = cluster.Spec.PodSpec.PodMetadata.Name
		}

		for key, value := range cluster.Spec.PodSpec.PodMetadata.Labels {
			podMetadata.Labels[key] = value
		}

		podMetadata.Annotations = cluster.Spec.PodSpec.PodMetadata.Annotations
	}

	podEnv := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
	podEnv = append(podEnv, cluster.Spec.PodSpec.ExtraEnv...)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Spec: appsv1.StatefulSetSpec{
			// initialize static fields that cannot be changed across updates.
			Replicas:            cluster.Spec.Replicas,
			ServiceName:         cluster.Name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMetadata,
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "etcd",
							Image:           cluster.Spec.PodSpec.Image,
							ImagePullPolicy: cluster.Spec.PodSpec.ImagePullPolicy,
							Command:         generateEtcdCommand(cluster),
							Ports: []corev1.ContainerPort{
								{Name: "peer", ContainerPort: 2380},
								{Name: "client", ContainerPort: 2379},
							},
							Resources: cluster.Spec.PodSpec.Resources,
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: GetClusterStateConfigMapName(cluster),
										},
									},
								},
							},
							Env: podEnv,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									ReadOnly:  false,
									MountPath: "/var/run/etcd",
								},
							},
							StartupProbe:   getStartupProbe(cluster.Spec.PodSpec.StartupProbe),
							LivenessProbe:  getLivenessProbe(cluster.Spec.PodSpec.LivenessProbe),
							ReadinessProbe: getReadinessProbe(cluster.Spec.PodSpec.ReadinessProbe),
						},
					},
					ImagePullSecrets:              cluster.Spec.PodSpec.ImagePullSecrets,
					Affinity:                      cluster.Spec.PodSpec.Affinity,
					NodeSelector:                  cluster.Spec.PodSpec.NodeSelector,
					TopologySpreadConstraints:     cluster.Spec.PodSpec.TopologySpreadConstraints,
					Tolerations:                   cluster.Spec.PodSpec.Tolerations,
					SecurityContext:               cluster.Spec.PodSpec.SecurityContext,
					PriorityClassName:             cluster.Spec.PodSpec.PriorityClassName,
					TerminationGracePeriodSeconds: cluster.Spec.PodSpec.TerminationGracePeriodSeconds,
					SchedulerName:                 cluster.Spec.PodSpec.SchedulerName,
					RuntimeClassName:              cluster.Spec.PodSpec.RuntimeClassName,
				},
			},
		},
	}
	if cluster.Spec.Storage.EmptyDir != nil {
		statefulSet.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name:         "data",
				VolumeSource: corev1.VolumeSource{EmptyDir: cluster.Spec.Storage.EmptyDir},
			},
		}
	} else {
		statefulSet.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: GetPVCName(cluster),
					},
				},
			},
		}
		statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        GetPVCName(cluster),
					Labels:      cluster.Spec.Storage.VolumeClaimTemplate.Labels,
					Annotations: cluster.Spec.Storage.VolumeClaimTemplate.Annotations,
				},
				Spec:   cluster.Spec.Storage.VolumeClaimTemplate.Spec,
				Status: cluster.Spec.Storage.VolumeClaimTemplate.Status,
			},
		}
	}

	if err := ctrl.SetControllerReference(cluster, statefulSet, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileStatefulSet(ctx, rclient, cluster.Name, statefulSet)
}

func generateEtcdCommand(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {
	command := []string{
		"etcd",
		"--name=$(POD_NAME)",
		"--listen-peer-urls=https://0.0.0.0:2380",
		// for first version disable TLS for client access
		"--listen-client-urls=http://0.0.0.0:2379",
		fmt.Sprintf("--initial-advertise-peer-urls=https://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2380", cluster.Name),
		"--data-dir=/var/run/etcd/default.etcd",
		"--auto-tls",
		"--peer-auto-tls",
		fmt.Sprintf("--advertise-client-urls=http://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2379", cluster.Name),
	}

	for name, value := range cluster.Spec.PodSpec.ExtraArgs {
		command = append(command, fmt.Sprintf("--%s=%s", name, value))
	}

	return command
}

func getStartupProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/readyz?serializable=false",
				Port: intstr.FromInt32(2379),
			},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func getReadinessProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/readyz",
				Port: intstr.FromInt32(2379),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func getLivenessProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/livez",
				Port: intstr.FromInt32(2379),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func mergeWithDefaultProbe(probe *corev1.Probe, defaultProbe *corev1.Probe) *corev1.Probe {
	if probe == nil {
		return defaultProbe
	}

	if probe.InitialDelaySeconds != 0 {
		defaultProbe.InitialDelaySeconds = probe.InitialDelaySeconds
	}

	if probe.PeriodSeconds != 0 {
		defaultProbe.PeriodSeconds = probe.PeriodSeconds
	}

	if hasProbeHandlerAction(*probe) {
		defaultProbe.ProbeHandler = probe.ProbeHandler
	}

	return defaultProbe
}

func hasProbeHandlerAction(probe corev1.Probe) bool {
	return probe.HTTPGet != nil || probe.TCPSocket != nil || probe.Exec != nil || probe.GRPC != nil
}
