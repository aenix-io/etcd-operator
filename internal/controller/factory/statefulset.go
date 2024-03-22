package factory

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

func CreateOrUpdateStatefulSet(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
) error {
	podMetadata := metav1.ObjectMeta{
		Labels: map[string]string{
			"app.kubernetes.io/name":       "etcd",
			"app.kubernetes.io/instance":   cluster.Name,
			"app.kubernetes.io/managed-by": "etcd-operator",
		},
	}

	if cluster.Spec.PodSpec.PodMetadata != nil {
		if cluster.Spec.PodSpec.PodMetadata.Name != "" {
			podMetadata.GenerateName = cluster.Spec.PodSpec.PodMetadata.Name
		}

		podMetadata.Labels = cluster.Spec.PodSpec.PodMetadata.Labels
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

	command := []string{
		"etcd",
		"--name=$(POD_NAME)",
		"--listen-peer-urls=https://0.0.0.0:2380",
		// for first version disable TLS for client access
		"--listen-client-urls=http://0.0.0.0:2379",
		"--initial-advertise-peer-urls=https://$(POD_NAME)." + cluster.Name + ".$(POD_NAMESPACE).svc:2380",
		"--data-dir=/var/run/etcd/default.etcd",
		"--auto-tls",
		"--peer-auto-tls",
		"--advertise-client-urls=http://$(POD_NAME)." + cluster.Name + ".$(POD_NAMESPACE).svc:2379",
	}

	for name, value := range cluster.Spec.PodSpec.ExtraArgs {
		command = append(command, fmt.Sprintf("--%s=%s", name, value))
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cluster.Namespace,
			Name:            cluster.Name,
			OwnerReferences: cluster.AsOwner(),
		},
		Spec: appsv1.StatefulSetSpec{
			// initialize static fields that cannot be changed across updates.
			Replicas:            cluster.Spec.Replicas,
			ServiceName:         cluster.Name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":       "etcd",
					"app.kubernetes.io/instance":   cluster.Name,
					"app.kubernetes.io/managed-by": "etcd-operator",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMetadata,
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "etcd",
							Image:           "quay.io/coreos/etcd:v3.5.12",
							ImagePullPolicy: cluster.Spec.PodSpec.ImagePullPolicy,
							Command:         command,
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
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz?serializable=false",
										Port: intstr.FromInt32(2379),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/livez",
										Port: intstr.FromInt32(2379),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt32(2379),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
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

	return reconcileSTS(ctx, rclient, cluster.Name, statefulSet)
}
