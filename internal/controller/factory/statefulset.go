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
	"path/filepath"
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

type certConfigurator interface {
	Name() string
	MountPath() string
	CrtFilePath() string
	KeyFilePath() string
}

type certificate struct {
	certName  string
	mountPath string
	crtFile   string
	keyFile   string
}

type certificates map[string]certConfigurator

const (
	etcdContainerName = "etcd"
	etcdVolumeName    = "data"
	etcdDataMountPath = "/var/run/etcd"
	ectdDataDirName   = "default.etcd"
)

var (
	etcdCertificates = certificates{
		"peer-trusted-ca-certificate": getCertificateConfig("peer-trusted-ca-certificate",
			"/etc/etcd/pki/peer/ca", "ca.crt", ""),
		"peer-certificate": getCertificateConfig("peer-certificate",
			"/etc/etcd/pki/peer/cert", "tls.crt", "tls.key"),
		"server-certificate": getCertificateConfig("server-certificate",
			"/etc/etcd/pki/server/cert", "tls.crt", "tls.key"),
		"client-trusted-ca-certificate": getCertificateConfig("client-trusted-ca-certificate",
			"/etc/etcd/pki/client/ca", "ca.crt", ""),
	}

	peerTrustedCACertificate   = etcdCertificates["peer-trusted-ca-certificate"]
	peerCertificate            = etcdCertificates["peer-certificate"]
	serverCertificate          = etcdCertificates["server-certificate"]
	clientTrustedCACertificate = etcdCertificates["client-trusted-ca-certificate"]

	etcdDataFullPath = filepath.Join(etcdDataMountPath, ectdDataDirName)
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
	containers := generateContainers(cluster)

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
					Containers:                    containers,
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

	dataVolume := generateDataVolume(cluster)
	volumes = append(volumes, dataVolume)

	secretVolumes := generateSecretVolumes(cluster)
	for _, volume := range secretVolumes {
		volumes = append(volumes, volume)
	}

	return volumes
}

func generateVolumeMounts(
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	containerVolumeMounts []corev1.VolumeMount,
) []corev1.VolumeMount {
	dataVolumeMount := updateOrAddEtcdDataVolumeMount(containerVolumeMounts)
	tlsVolumeMounts := generateTLSSecretVolumeMounts(cluster)
	volumeMounts := mergeVolumeMounts(tlsVolumeMounts, dataVolumeMount)

	return volumeMounts
}

func generateEtcdCommand() []string {
	return []string{
		"etcd",
	}
}

func generateEtcdArgs(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {
	args := baseEtcdFlags()

	// Append TLS flags
	args = append(args, etcdTLSFlags(cluster)...)
	// Append URLs flags
	args = append(args, etcdURLsFlags(cluster)...)
	// Append extra flags
	if cluster.Spec.Options != nil {
		args = append(args, etcdExtraFlags(cluster.Spec.Options)...)
	}

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
				return env.ConfigMapRef != nil &&
					env.ConfigMapRef.LocalObjectReference.Name == clusterStateConfigMapName
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
			c.VolumeMounts = generateVolumeMounts(cluster, c.VolumeMounts)
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

// Name returns the name of the certificate.
func (c certificate) Name() string {
	return c.certName
}

// MountPath returns the mount path associated with the certificate.
func (c certificate) MountPath() string {
	return c.mountPath
}

// CrtFilePath returns the file path of the certificate's CRT file.
func (c certificate) CrtFilePath() string {
	return c.buildFilePath(c.crtFile)
}

// KeyFilePath returns the file path of the certificate's key file, if available.
func (c certificate) KeyFilePath() string {
	if c.keyFile == "" {
		return ""
	}
	return c.buildFilePath(c.keyFile)
}

// buildFilePath constructs the full file path using the certificate's mount path.
func (c certificate) buildFilePath(fileName string) string {
	return filepath.Join(c.mountPath, fileName)
}

// getCertificateConfig returns a certificate configuration object with the provided details.
func getCertificateConfig(name, mountPath, crtFile, keyFile string) certificate {
	return certificate{
		certName:  name,
		mountPath: mountPath,
		crtFile:   crtFile,
		keyFile:   keyFile,
	}
}

// hasTLSConfigInSpec checks if a specific type of TLS secret is configured in the cluster specification.
func hasTLSConfigInSpec(cluster *etcdaenixiov1alpha1.EtcdCluster, secretType string) bool {
	if cluster.Spec.Security == nil {
		return false
	}

	switch secretType {
	case "PeerSecret":
		return cluster.Spec.Security.TLS.PeerSecret != ""
	case "ServerSecret":
		return cluster.Spec.Security.TLS.ServerSecret != ""
	case "ClientSecret":
		return cluster.Spec.Security.TLS.ClientSecret != ""
	default:
		return false
	}
}

// baseEtcdFlags returns a list of base flags for configuring etcd.
func baseEtcdFlags() []string {
	return []string{
		"--name=$(POD_NAME)",
		"--listen-metrics-urls=http://0.0.0.0:2381",
		"--listen-peer-urls=https://0.0.0.0:2380",
		"--data-dir=" + etcdDataFullPath,
	}
}

// etcdTLSFlags generates TLS-related flags for etcd based on the cluster's TLS configuration.
func etcdTLSFlags(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {
	args := []string{}

	if hasTLSConfigInSpec(cluster, "PeerSecret") {
		args = append(args,
			"--peer-trusted-ca-file="+peerTrustedCACertificate.CrtFilePath(),
			"--peer-cert-file="+peerCertificate.CrtFilePath(),
			"--peer-key-file="+peerCertificate.KeyFilePath(),
			"--peer-client-cert-auth",
		)
	} else {
		args = append(args,
			"--peer-auto-tls",
		)
	}
	if hasTLSConfigInSpec(cluster, "ServerSecret") {
		args = append(args,
			"--cert-file="+serverCertificate.CrtFilePath(),
			"--key-file="+serverCertificate.KeyFilePath(),
		)
	}
	if hasTLSConfigInSpec(cluster, "ClientSecret") {
		args = append(args,
			"--trusted-ca-file="+clientTrustedCACertificate.CrtFilePath(),
			"--client-cert-auth",
		)
	}

	return args
}

// etcdURLsFlags generates URL-related flags for etcd depending on the TLS cluster configuration.
func etcdURLsFlags(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {
	args := []string{}

	clientProtocol := "http"
	if hasTLSConfigInSpec(cluster, "ServerSecret") {
		clientProtocol = "https"
	}

	listenClientURL := fmt.Sprintf("%s://0.0.0.0:2379", clientProtocol)
	advertiseClientURL := fmt.Sprintf("%s://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2379",
		clientProtocol, cluster.Name)
	initialAdvertisePeerURL := fmt.Sprintf("https://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2380", cluster.Name)

	args = append(args, "--listen-client-urls="+listenClientURL)
	args = append(args, "--advertise-client-urls="+advertiseClientURL)
	args = append(args, "--initial-advertise-peer-urls="+initialAdvertisePeerURL)

	return args
}

// etcdExtraFlags generates additional flags for etcd based on the provided in spec options.
func etcdExtraFlags(options map[string]string) []string {
	args := []string{}

	for name, value := range options {
		flag := "--" + name
		if len(value) == 0 {
			args = append(args, flag)
		} else {
			args = append(args, fmt.Sprintf("%s=%s", flag, value))
		}
	}
	return args
}

// generateSecretVolumes generates secret volumes based on TLS configuration in the etcd cluster spec.
func generateSecretVolumes(cluster *etcdaenixiov1alpha1.EtcdCluster) map[string]corev1.Volume {
	volumesMap := make(map[string]corev1.Volume)

	addSecretVolume := func(name, secretName string) {
		volumesMap[name] = createSecretVolume(name, secretName)
	}

	if cluster.Spec.Security != nil {
		if cluster.Spec.Security.TLS.PeerSecret != "" {
			addSecretVolume(peerTrustedCACertificate.Name(), cluster.Spec.Security.TLS.PeerTrustedCASecret)
			addSecretVolume(peerCertificate.Name(), cluster.Spec.Security.TLS.PeerSecret)
		}
		if cluster.Spec.Security.TLS.ServerSecret != "" {
			addSecretVolume(serverCertificate.Name(), cluster.Spec.Security.TLS.ServerSecret)
		}
		if cluster.Spec.Security.TLS.ClientSecret != "" {
			addSecretVolume(clientTrustedCACertificate.Name(), cluster.Spec.Security.TLS.ClientTrustedCASecret)
		}
	}

	return volumesMap
}

// createSecretVolume return a volume object for a given secret.
func createSecretVolume(name, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

// generateDataVolume generates the etcd data volume.
func generateDataVolume(cluster *etcdaenixiov1alpha1.EtcdCluster) corev1.Volume {
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

	return corev1.Volume{
		Name:         etcdVolumeName,
		VolumeSource: dataVolumeSource,
	}
}

// updateOrAddEtcdDataVolumeMount updates or adds the volume mount for etcd data volume.
func updateOrAddEtcdDataVolumeMount(volumeMounts []corev1.VolumeMount) []corev1.VolumeMount {
	readOnly := false

	// Check if the volume mount with etcdVolumeName already exists
	idx := slices.IndexFunc(volumeMounts, func(vm corev1.VolumeMount) bool {
		return vm.Name == etcdVolumeName
	})

	if idx >= 0 {
		// Volume mount with etcdVolumeName found, update it
		volumeMounts[idx].ReadOnly = readOnly
		volumeMounts[idx].MountPath = etcdDataMountPath
	} else {
		// Volume mount with etcdVolumeName not found, append new volume mount
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      etcdVolumeName,
			ReadOnly:  readOnly,
			MountPath: etcdDataMountPath,
		})
	}

	return volumeMounts
}

// generateTLSSecretVolumeMounts generates volume mounts for TLS secrets.
func generateTLSSecretVolumeMounts(cluster *etcdaenixiov1alpha1.EtcdCluster) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{}

	if hasTLSConfigInSpec(cluster, "") {
		return volumeMounts
	}

	if hasTLSConfigInSpec(cluster, "PeerSecret") {
		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      peerTrustedCACertificate.Name(),
				ReadOnly:  true,
				MountPath: peerTrustedCACertificate.MountPath(),
			},
			{
				Name:      peerCertificate.Name(),
				ReadOnly:  true,
				MountPath: peerCertificate.MountPath(),
			},
		}...)
	}

	if hasTLSConfigInSpec(cluster, "ServerSecret") {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      serverCertificate.Name(),
			ReadOnly:  true,
			MountPath: serverCertificate.MountPath(),
		})
	}

	if hasTLSConfigInSpec(cluster, "ClientSecret") {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      clientTrustedCACertificate.Name(),
			ReadOnly:  true,
			MountPath: clientTrustedCACertificate.MountPath(),
		})
	}

	return volumeMounts
}

// mergeVolumeMounts merges multiple lists of volume mounts into a single list.
func mergeVolumeMounts(lists ...[]corev1.VolumeMount) []corev1.VolumeMount {
	var volumeMounts []corev1.VolumeMount

	for _, list := range lists {
		volumeMounts = append(volumeMounts, list...)
	}

	return volumeMounts
}
