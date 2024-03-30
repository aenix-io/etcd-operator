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

package webhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller/factory"
)

// log is for logging in this package.
var etcdclusterlog = logf.Log.WithName("etcdcluster-resource")

type EtcdCluster struct{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *EtcdCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1alpha1.EtcdCluster{}).
		WithValidator(r).
		WithDefaulter(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=metcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.CustomDefaulter = &EtcdCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *EtcdCluster) Default(_ context.Context, obj runtime.Object) error {
	etcdCluster, ok := obj.(*v1alpha1.EtcdCluster)
	if !ok {
		return fmt.Errorf("expected a EtcdCluster but got a %T", obj)
	}

	etcdclusterlog.Info("default", "name", etcdCluster.Name)
	if len(etcdCluster.Spec.PodSpec.Image) == 0 {
		etcdCluster.Spec.PodSpec.Image = v1alpha1.DefaultEtcdImage
	}
	if etcdCluster.Spec.Storage.EmptyDir == nil {
		if len(etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.AccessModes) == 0 {
			etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		}
		storage := etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()
		if storage == nil || storage.IsZero() {
			etcdCluster.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("4Gi"),
			}
		}
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=vetcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &EtcdCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	etcdCluster, ok := obj.(*v1alpha1.EtcdCluster)
	if !ok {
		return nil, fmt.Errorf("expected a EtcdCluster but got a %T", obj)
	}

	etcdclusterlog.Info("validate create", "name", etcdCluster.Name)

	return nil, factory.ValidatePodExtraArgs(etcdCluster)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldCluster, ok := oldObj.(*v1alpha1.EtcdCluster)
	if !ok {
		return nil, fmt.Errorf("expected a EtcdCluster but got a %T", oldObj)
	}

	newCluster, ok := newObj.(*v1alpha1.EtcdCluster)
	if !ok {
		return nil, fmt.Errorf("expected a EtcdCluster but got a %T", newObj)
	}

	etcdclusterlog.Info("validate update", "name", newCluster.Name)

	var warnings admission.Warnings

	if *oldCluster.Spec.Replicas != *newCluster.Spec.Replicas {
		warnings = append(warnings, "cluster resize is not currently supported")
	}

	var allErrors field.ErrorList
	if oldCluster.Spec.Storage.EmptyDir == nil && newCluster.Spec.Storage.EmptyDir != nil ||
		oldCluster.Spec.Storage.EmptyDir != nil && newCluster.Spec.Storage.EmptyDir == nil {
		allErrors = append(allErrors, field.Invalid(
			field.NewPath("spec", "storage", "emptyDir"),
			newCluster.Spec.Storage.EmptyDir,
			"field is immutable"),
		)
	}

	if len(allErrors) > 0 {
		err := errors.NewInvalid(
			schema.GroupKind{Group: v1alpha1.GroupVersion.Group, Kind: "EtcdCluster"},
			newCluster.Name, allErrors)
		return warnings, err
	}

	return warnings, factory.ValidatePodExtraArgs(newCluster)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	etcdCluster, ok := obj.(*v1alpha1.EtcdCluster)
	if !ok {
		return nil, fmt.Errorf("expected a EtcdCluster but got a %T", obj)
	}

	etcdclusterlog.Info("validate delete", "name", etcdCluster.Name)
	return nil, nil
}
