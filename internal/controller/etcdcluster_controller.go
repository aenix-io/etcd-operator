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

	policyv1 "k8s.io/api/policy/v1"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
// +kubebuilder:rbac:groups="policy",resources=poddisruptionbudgets,verbs=get;create;delete;update;patch;list;watch

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

	// fill conditions
	if len(instance.Status.Conditions) == 0 {
		factory.FillConditions(instance)
	}

	// ensure managed resources
	if err := r.ensureClusterObjects(ctx, instance); err != nil {
		logger.Error(err, "cannot create Cluster auxiliary objects")
		return r.updateStatusOnErr(ctx, instance, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err))
	}

	// set cluster initialization condition
	factory.SetCondition(instance, factory.NewCondition(etcdaenixiov1alpha1.EtcdConditionInitialized).
		WithStatus(true).
		WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete)).
		WithMessage(string(etcdaenixiov1alpha1.EtcdInitCondPosMessage)).
		Complete())

	// check sts condition
	clusterReady, err := r.isStatefulSetReady(ctx, instance)
	if err != nil {
		logger.Error(err, "failed to check etcd cluster state")
		return r.updateStatusOnErr(ctx, instance, fmt.Errorf("cannot check Cluster readiness: %w", err))
	}

	// set cluster readiness condition
	existingCondition := factory.GetCondition(instance, etcdaenixiov1alpha1.EtcdConditionReady)
	if existingCondition.Reason == string(etcdaenixiov1alpha1.EtcdCondTypeWaitingForFirstQuorum) {
		// we should change from "waiting for first quorum establishment" to "StatefulSet ready / not ready"
		// only after sts gets ready first time
		if clusterReady {
			factory.SetCondition(instance, factory.NewCondition(etcdaenixiov1alpha1.EtcdConditionReady).
				WithStatus(true).
				WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady)).
				WithMessage(string(etcdaenixiov1alpha1.EtcdReadyCondPosMessage)).
				Complete())
		}
	} else {
		reason := etcdaenixiov1alpha1.EtcdCondTypeStatefulSetNotReady
		message := etcdaenixiov1alpha1.EtcdReadyCondNegMessage
		if clusterReady {
			reason = etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady
			message = etcdaenixiov1alpha1.EtcdReadyCondPosMessage
		}

		factory.SetCondition(instance, factory.NewCondition(etcdaenixiov1alpha1.EtcdConditionReady).
			WithStatus(clusterReady).
			WithReason(string(reason)).
			WithMessage(string(message)).
			Complete())
	}
	return r.updateStatus(ctx, instance)
}

// ensureClusterObjects creates or updates all objects owned by cluster CR
func (r *EtcdClusterReconciler) ensureClusterObjects(
	ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) error {
	if err := factory.CreateOrUpdateClusterStateConfigMap(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	if err := factory.CreateOrUpdateClusterService(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	if err := factory.CreateOrUpdateStatefulSet(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	if err := factory.CreateOrUpdateClientService(ctx, cluster, r.Client, r.Scheme); err != nil {
		return err
	}
	if err := factory.CreateOrUpdatePdb(ctx, cluster, r.Client, r.Scheme); err != nil {
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

// isStatefulSetReady gets managed StatefulSet and checks its readiness.
func (r *EtcdClusterReconciler) isStatefulSetReady(ctx context.Context, c *etcdaenixiov1alpha1.EtcdCluster) (bool, error) {
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(c), sts)
	if err == nil {
		return sts.Status.ReadyReplicas == *sts.Spec.Replicas, nil
	}
	return false, client.IgnoreNotFound(err)
}

// SetupWithManager sets up the controller with the Manager.
func (r *EtcdClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdaenixiov1alpha1.EtcdCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Complete(r)
}
