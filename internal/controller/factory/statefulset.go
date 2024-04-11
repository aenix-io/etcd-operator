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
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

const (
	etcdContainerName = "etcd"
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

	if cluster.Spec.PodTemplate.Name != "" {
		podMetadata.GenerateName = cluster.Spec.PodTemplate.Name
	}

	for key, value := range cluster.Spec.PodTemplate.Labels {
		podMetadata.Labels[key] = value
	}

	podMetadata.Annotations = cluster.Spec.PodTemplate.Annotations

	volumeClaimTemplates := []corev1.PersistentVolumeClaim{
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

	volumes := generateVolumes(cluster)

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
					Containers:                    generateContainers(cluster),
					ImagePullSecrets:              cluster.Spec.PodTemplate.Spec.ImagePullSecrets,
					Affinity:                      cluster.Spec.PodTemplate.Spec.Affinity,
					NodeSelector:                  cluster.Spec.PodTemplate.Spec.NodeSelector,
					TopologySpreadConstraints:     cluster.Spec.PodTemplate.Spec.TopologySpreadConstraints,
					Tolerations:                   cluster.Spec.PodTemplate.Spec.Tolerations,
					SecurityContext:               cluster.Spec.PodTemplate.Spec.SecurityContext,
					PriorityClassName:             cluster.Spec.PodTemplate.Spec.PriorityClassName,
					TerminationGracePeriodSeconds: cluster.Spec.PodTemplate.Spec.TerminationGracePeriodSeconds,
					SchedulerName:                 cluster.Spec.PodTemplate.Spec.SchedulerName,
					ServiceAccountName:            cluster.Spec.PodTemplate.Spec.ServiceAccountName,
					ReadinessGates:                cluster.Spec.PodTemplate.Spec.ReadinessGates,
					RuntimeClassName:              cluster.Spec.PodTemplate.Spec.RuntimeClassName,
					Volumes:                       volumes,
				},
			},
			VolumeClaimTemplates: volumeClaimTemplates,
		},
	}

	if err := ctrl.SetControllerReference(cluster, statefulSet, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileStatefulSet(ctx, rclient, cluster.Name, statefulSet)
}

func generateVolumes(cluster *etcdaenixiov1alpha1.EtcdCluster) []corev1.Volume {
	volumes := []corev1.Volume{}

	var dataVolumeSource corev1.VolumeSource

	if cluster.Spec.Storage.EmptyDir != nil {
		dataVolumeSource = corev1.VolumeSource{EmptyDir: cluster.Spec.Storage.EmptyDir}
	} else {
		dataVolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: GetPVCName(cluster),
			},
		}
	}

	dataVolumeIdx := slices.IndexFunc(cluster.Spec.PodTemplate.Spec.Volumes, func(volume corev1.Volume) bool {
		return volume.Name == "data"
	})
	if dataVolumeIdx == -1 {
		dataVolumeIdx = len(cluster.Spec.PodTemplate.Spec.Volumes)
		volumes = append(
			volumes,
			corev1.Volume{},
		)
	}

	volumes[dataVolumeIdx] = corev1.Volume{
		Name:         "data",
		VolumeSource: dataVolumeSource,
	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.PeerSecret != "" {
		volumes = append(volumes,
			[]corev1.Volume{
				{
					Name: "peer-trusted-ca-certificate",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.TLS.PeerTrustedCASecret,
						},
					},
				},
				{
					Name: "peer-certificate",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.TLS.PeerSecret,
						},
					},
				},
			}...)
	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ServerSecret != "" {
		volumes = append(volumes,
			[]corev1.Volume{
				{
					Name: "server-certificate",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.TLS.ServerSecret,
						},
					},
				},
			}...)
	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ClientSecret != "" {
		volumes = append(volumes,
			[]corev1.Volume{
				{
					Name: "client-trusted-ca-certificate",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.TLS.ClientTrustedCASecret,
						},
					},
				},
			}...)
	}

	return volumes

}

func generateVolumeMounts(cluster *etcdaenixiov1alpha1.EtcdCluster) []corev1.VolumeMount {

	volumeMounts := []corev1.VolumeMount{}

	for _, c := range cluster.Spec.PodTemplate.Spec.Containers {
		if c.Name == etcdContainerName {

			volumeMounts = c.VolumeMounts

			mountIdx := slices.IndexFunc(volumeMounts, func(mount corev1.VolumeMount) bool {
				return mount.Name == "data"
			})
			if mountIdx == -1 {
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      "data",
					ReadOnly:  false,
					MountPath: "/var/run/etcd",
				})
			} else {
				volumeMounts[mountIdx].ReadOnly = false
				volumeMounts[mountIdx].MountPath = "/var/run/etcd"
			}
		}

	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.PeerSecret != "" {
		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      "peer-trusted-ca-certificate",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/peer/ca",
			},
			{
				Name:      "peer-certificate",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/peer/cert",
			},
		}...)
	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ServerSecret != "" {
		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      "server-certificate",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/server/cert",
			},
		}...)
	}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ClientSecret != "" {

		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      "client-trusted-ca-certificate",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/client/ca",
			},
		}...)
	}

	return volumeMounts
}

func generateEtcdCommand() []string {
	return []string{
		"etcd",
	}
}

func generateEtcdArgs(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {
	args := []string{}

	for name, value := range cluster.Spec.Options {
		flag := "--" + name
		if len(value) == 0 {
			args = append(args, flag)

			continue
		}

		args = append(args, fmt.Sprintf("%s=%s", flag, value))
	}

	peerTlsSettings := []string{"--peer-auto-tls"}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.PeerSecret != "" {
		peerTlsSettings = []string{
			"--peer-trusted-ca-file=/etc/etcd/pki/peer/ca/ca.crt",
			"--peer-cert-file=/etc/etcd/pki/peer/cert/tls.crt",
			"--peer-key-file=/etc/etcd/pki/peer/cert/tls.key",
			"--peer-client-cert-auth",
		}
	}

	serverTlsSettings := []string{}
	serverProtocol := "http"

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ServerSecret != "" {
		serverTlsSettings = []string{
			"--cert-file=/etc/etcd/pki/server/cert/tls.crt",
			"--key-file=/etc/etcd/pki/server/cert/tls.key",
		}
		serverProtocol = "https"
	}

	clientTlsSettings := []string{}

	if cluster.Spec.Security != nil && cluster.Spec.Security.TLS.ClientSecret != "" {
		clientTlsSettings = []string{
			"--trusted-ca-file=/etc/etcd/pki/client/ca/ca.crt",
			"--client-cert-auth",
		}
	}

	args = append(args, []string{
		"--name=$(POD_NAME)",
		"--listen-metrics-urls=http://0.0.0.0:2381",
		"--listen-peer-urls=https://0.0.0.0:2380",
		fmt.Sprintf("--listen-client-urls=%s://0.0.0.0:2379", serverProtocol),
		fmt.Sprintf("--initial-advertise-peer-urls=https://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2380", cluster.Name),
		"--data-dir=/var/run/etcd/default.etcd",
		fmt.Sprintf("--advertise-client-urls=%s://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2379", serverProtocol, cluster.Name),
	}...)

	args = append(args, peerTlsSettings...)
	args = append(args, serverTlsSettings...)
	args = append(args, clientTlsSettings...)

	return args
}

func generateContainers(cluster *etcdaenixiov1alpha1.EtcdCluster) []corev1.Container {
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

	containers := make([]corev1.Container, 0, len(cluster.Spec.PodTemplate.Spec.Containers))
	for _, c := range cluster.Spec.PodTemplate.Spec.Containers {
		if c.Name == etcdContainerName {
			c.Command = generateEtcdCommand()
			c.Args = generateEtcdArgs(cluster)
			c.Ports = mergePorts(c.Ports, []corev1.ContainerPort{
				{Name: "peer", ContainerPort: 2380},
				{Name: "client", ContainerPort: 2379},
			})
			clusterStateConfigMapName := GetClusterStateConfigMapName(cluster)
			envIdx := slices.IndexFunc(c.EnvFrom, func(env corev1.EnvFromSource) bool {
				return env.ConfigMapRef != nil && env.ConfigMapRef.LocalObjectReference.Name == clusterStateConfigMapName
			})
			if envIdx == -1 {
				c.EnvFrom = append(c.EnvFrom, corev1.EnvFromSource{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: clusterStateConfigMapName,
						},
					},
				})
			}
			c.StartupProbe = getStartupProbe(c.StartupProbe)
			c.LivenessProbe = getLivenessProbe(c.LivenessProbe)
			c.ReadinessProbe = getReadinessProbe(c.ReadinessProbe)
			c.Env = mergeEnvs(c.Env, podEnv)
			c.VolumeMounts = generateVolumeMounts(cluster)
		}

		containers = append(containers, c)
	}

	return containers
}

// mergePorts uses both "old" and "new" values and replaces collisions by name with "new".
func mergePorts(old []corev1.ContainerPort, new []corev1.ContainerPort) []corev1.ContainerPort {
	ports := make(map[string]corev1.ContainerPort, len(old))
	for _, p := range old {
		ports[p.Name] = p
	}
	for _, p := range new {
		ports[p.Name] = p
	}

	mergedPorts := make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		mergedPorts = append(mergedPorts, port)
	}
	return mergedPorts
}

// mergeEnvs uses both "old" and "new" values and replaces collisions by name with "new".
func mergeEnvs(old []corev1.EnvVar, new []corev1.EnvVar) []corev1.EnvVar {
	envs := make(map[string]corev1.EnvVar, len(old))
	for _, env := range old {
		envs[env.Name] = env
	}
	for _, env := range new {
		envs[env.Name] = env
	}

	merged := make([]corev1.EnvVar, 0, len(envs))
	for _, env := range envs {
		merged = append(merged, env)
	}
	return merged
}

func getStartupProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/readyz?serializable=false",
				Port: intstr.FromInt32(2381),
			},
		},
		PeriodSeconds: 5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func getReadinessProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/readyz",
				Port: intstr.FromInt32(2381),
			},
		},
		PeriodSeconds: 5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func getLivenessProbe(probe *corev1.Probe) *corev1.Probe {
	defaultProbe := corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/livez",
				Port: intstr.FromInt32(2381),
			},
		},
		PeriodSeconds: 5,
	}
	return mergeWithDefaultProbe(probe, defaultProbe)
}

func mergeWithDefaultProbe(probe *corev1.Probe, defaultProbe corev1.Probe) *corev1.Probe {
	if probe == nil {
		return &defaultProbe
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

	return &defaultProbe
}

func hasProbeHandlerAction(probe corev1.Probe) bool {
	return probe.HTTPGet != nil || probe.TCPSocket != nil || probe.Exec != nil || probe.GRPC != nil
}
