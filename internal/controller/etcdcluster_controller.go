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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller/factory"
)

// EtcdClusterReconciler reconciles a EtcdCluster object
type EtcdClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;create;delete;update;patch;list;watch

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
	ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster, isClusterReady bool) error {
	// 1. create or update configmap <name>-cluster-state
	if err := factory.CreateOrUpdateClusterStateConfigMap(ctx, cluster, isClusterReady, r.Client, r.Scheme); err != nil {
		return err
	}
	if err := factory.CreateOrUpdateClusterService(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	// 2. create or update statefulset
	if err := factory.CreateOrUpdateStatefulSet(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	// 3. create or update ClusterIP Service
	if err := factory.CreateOrUpdateClientService(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}

	return nil
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
