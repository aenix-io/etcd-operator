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
	goerrors "errors"
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

// EtcdClusterReconciler reconciles a EtcdCluster object
type EtcdClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
//+kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;create;delete;update;patch;list;watch

// Reconcile checks CR and current cluster state and performs actions to transform current state to desired.
func (r *EtcdClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(2).Info("reconciling object", "namespaced_name", req.NamespacedName)
	instance := &etcdaenixiov1alpha1.EtcdCluster{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("object not found", "namespaced_name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		// Error retrieving object, requeue
		return reconcile.Result{}, err
	}
	// If object is being deleted, skipping reconciliation
	if !instance.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	// 3. mark CR as initialized
	if len(instance.Status.Conditions) == 0 {
		instance.Status.Conditions = append(instance.Status.Conditions, metav1.Condition{
			Type:               etcdaenixiov1alpha1.EtcdConditionInitialized,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: instance.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted),
			Message:            "Cluster initialization has started",
		})
	}

	// check sts condition
	isClusterReady := false
	sts := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, sts)
	if err == nil {
		isClusterReady = sts.Status.ReadyReplicas == *sts.Spec.Replicas
	}

	if err := r.ensureClusterObjects(ctx, instance, isClusterReady); err != nil {
		logger.Error(err, "cannot create Cluster auxiliary objects")
		return r.updateStatusOnErr(ctx, instance, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err))
	}

	r.updateClusterState(instance, metav1.Condition{
		Type:               etcdaenixiov1alpha1.EtcdConditionInitialized,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete),
		Message:            "Cluster initialization is complete",
	})
	if isClusterReady {
		r.updateClusterState(instance, metav1.Condition{
			Type:               etcdaenixiov1alpha1.EtcdConditionReady,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady),
			Message:            "Cluster StatefulSet is Ready",
		})
	} else {
		r.updateClusterState(instance, metav1.Condition{
			Type:               etcdaenixiov1alpha1.EtcdConditionReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetNotReady),
			Message:            "Cluster StatefulSet is not Ready",
		})
	}

	return r.updateStatus(ctx, instance)
}

// ensureClusterObjects creates or updates all objects owned by cluster CR
func (r *EtcdClusterReconciler) ensureClusterObjects(
	ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, isClusterInitialized bool) error {
	// 1. create or update configmap <name>-cluster-state
	if err := r.ensureClusterStateConfigMap(ctx, cluster, isClusterInitialized); err != nil {
		return err
	}
	if err := r.ensureClusterService(ctx, cluster); err != nil {
		return err
	}
	// 2. create or update statefulset
	if err := r.ensureClusterStatefulSet(ctx, cluster); err != nil {
		return err
	}
	// 3. create or update ClusterIP Service
	if err := r.ensureClusterClientService(ctx, cluster); err != nil {
		return err
	}

	return nil
}

func (r *EtcdClusterReconciler) ensureClusterService(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) error {
	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}, svc)
	// Service exists, skip creation
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("cannot get cluster service: %w", err)
	}

	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "etcd",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/managed-by": "etcd-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "peer", TargetPort: intstr.FromInt32(2380), Port: 2380, Protocol: corev1.ProtocolTCP},
				{Name: "client", TargetPort: intstr.FromInt32(2379), Port: 2379, Protocol: corev1.ProtocolTCP},
			},
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector: map[string]string{
				"app.kubernetes.io/name":       "etcd",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/managed-by": "etcd-operator",
			},
			PublishNotReadyAddresses: true,
		},
	}
	if err = ctrl.SetControllerReference(cluster, svc, r.Scheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}
	if err = r.Create(ctx, svc); err != nil {
		return fmt.Errorf("cannot create cluster service: %w", err)
	}
	return nil
}

func (r *EtcdClusterReconciler) ensureClusterClientService(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) error {
	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      r.getClientServiceName(cluster),
	}, svc)
	// Service exists, skip creation
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("cannot get cluster client service: %w", err)
	}

	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.getClientServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "etcd",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/managed-by": "etcd-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "client", TargetPort: intstr.FromInt32(2379), Port: 2379, Protocol: corev1.ProtocolTCP},
			},
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app.kubernetes.io/name":       "etcd",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/managed-by": "etcd-operator",
			},
		},
	}
	if err = ctrl.SetControllerReference(cluster, svc, r.Scheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}
	if err = r.Create(ctx, svc); err != nil {
		return fmt.Errorf("cannot create cluster client service: %w", err)
	}
	return nil
}

// ensureClusterStateConfigMap creates or updates cluster state configmap.
func (r *EtcdClusterReconciler) ensureClusterStateConfigMap(
	ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, isClusterInitialized bool) error {
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      r.getClusterStateConfigMapName(cluster),
	}, configMap)
	// configmap exists, skip editing.
	if err == nil {
		if isClusterInitialized {
			// update cluster state to existing
			configMap.Data["ETCD_INITIAL_CLUSTER_STATE"] = "existing"
			if err = r.Update(ctx, configMap); err != nil {
				return fmt.Errorf("cannot update cluster state configmap: %w", err)
			}
		}
		return nil
	}

	// configmap does not exist, create with cluster state "new"
	if errors.IsNotFound(err) {
		initialCluster := ""
		for i := int32(0); i < *cluster.Spec.Replicas; i++ {
			if i > 0 {
				initialCluster += ","
			}
			initialCluster += fmt.Sprintf("%s-%d=https://%s-%d.%s.%s.svc:2380",
				cluster.Name, i,
				cluster.Name, i, cluster.Name, cluster.Namespace,
			)
		}

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      r.getClusterStateConfigMapName(cluster),
			},
			Data: map[string]string{
				"ETCD_INITIAL_CLUSTER_STATE": "new",
				"ETCD_INITIAL_CLUSTER":       initialCluster,
				"ETCD_INITIAL_CLUSTER_TOKEN": cluster.Name + "-" + cluster.Namespace,
			},
		}
		if err := ctrl.SetControllerReference(cluster, configMap, r.Scheme); err != nil {
			return fmt.Errorf("cannot set controller reference: %w", err)
		}
		if err := r.Create(ctx, configMap); err != nil {
			return fmt.Errorf("cannot create cluster state configmap: %w", err)
		}
		return nil
	}

	return fmt.Errorf("cannot get cluster state configmap: %w", err)
}

func (r *EtcdClusterReconciler) ensureClusterStatefulSet(
	ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) error {
	statefulSet := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}, statefulSet)

	// statefulset does not exist, create new one
	notFound := false
	if errors.IsNotFound(err) {
		notFound = true
		// prepare initial cluster members

		statefulSet = &appsv1.StatefulSet{
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
					MatchLabels: map[string]string{
						"app.kubernetes.io/name":       "etcd",
						"app.kubernetes.io/instance":   cluster.Name,
						"app.kubernetes.io/managed-by": "etcd-operator",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/name":       "etcd",
							"app.kubernetes.io/instance":   cluster.Name,
							"app.kubernetes.io/managed-by": "etcd-operator",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "etcd",
								Image: "quay.io/coreos/etcd:v3.5.12",
								Command: []string{
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
								},
								Ports: []corev1.ContainerPort{
									{Name: "peer", ContainerPort: 2380},
									{Name: "client", ContainerPort: 2379},
								},
								EnvFrom: []corev1.EnvFromSource{
									{
										ConfigMapRef: &corev1.ConfigMapEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: r.getClusterStateConfigMapName(cluster),
											},
										},
									},
								},
								Env: []corev1.EnvVar{
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
								},
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
							ClaimName: r.getPVCName(cluster),
						},
					},
				},
			}
			statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        r.getPVCName(cluster),
						Labels:      cluster.Spec.Storage.VolumeClaimTemplate.Labels,
						Annotations: cluster.Spec.Storage.VolumeClaimTemplate.Annotations,
					},
					Spec:   cluster.Spec.Storage.VolumeClaimTemplate.Spec,
					Status: cluster.Spec.Storage.VolumeClaimTemplate.Status,
				},
			}
		}
		if err := ctrl.SetControllerReference(cluster, statefulSet, r.Scheme); err != nil {
			return fmt.Errorf("cannot set controller reference: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("cannot get cluster statefulset: %w", err)
	}

	if notFound {
		if err := r.Create(ctx, statefulSet); err != nil {
			return fmt.Errorf("cannot create statefulset: %w", err)
		}
	} else {
		if err := r.Update(ctx, statefulSet); err != nil {
			return fmt.Errorf("cannot update statefulset: %w", err)
		}
	}

	return nil
}

func (r *EtcdClusterReconciler) getClusterStateConfigMapName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	return cluster.Name + "-cluster-state"
}

func (r *EtcdClusterReconciler) getClientServiceName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	return cluster.Name + "-client"
}

func (r *EtcdClusterReconciler) getPVCName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if len(cluster.Spec.Storage.VolumeClaimTemplate.Name) > 0 {
		return cluster.Spec.Storage.VolumeClaimTemplate.Name
	}

	return "data"
}

// updateStatusOnErr wraps error and updates EtcdCluster status
func (r *EtcdClusterReconciler) updateStatusOnErr(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, err error) (ctrl.Result, error) {
	res, statusErr := r.updateStatus(ctx, cluster)
	if statusErr != nil {
		return res, goerrors.Join(statusErr, err)
	}
	return res, err
}

// updateStatus updates EtcdCluster status and returns error and requeue in case status could not be updated due to conflict
func (r *EtcdClusterReconciler) updateStatus(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "unable to update cluster status")
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// updateClusterState patches status condition in cluster using merge by Type
func (r *EtcdClusterReconciler) updateClusterState(cluster *etcdaenixiov1alpha1.EtcdCluster, state metav1.Condition) {
	if initIdx := slices.IndexFunc(cluster.Status.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == state.Type
	}); initIdx != -1 {
		cluster.Status.Conditions[initIdx].Status = state.Status
		cluster.Status.Conditions[initIdx].LastTransitionTime = state.LastTransitionTime
		cluster.Status.Conditions[initIdx].ObservedGeneration = cluster.Generation
		cluster.Status.Conditions[initIdx].Reason = state.Reason
		cluster.Status.Conditions[initIdx].Message = state.Message
	} else {
		state.ObservedGeneration = cluster.Generation
		cluster.Status.Conditions = append(cluster.Status.Conditions, state)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *EtcdClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdaenixiov1alpha1.EtcdCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
