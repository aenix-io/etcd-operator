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

package v1alpha1

import (
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
)

// log is for logging in this package.
var etcdclusterlog = logf.Log.WithName("etcdcluster-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *EtcdCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=metcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &EtcdCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *EtcdCluster) Default() {
	etcdclusterlog.Info("default", "name", r.Name)
	if r.Spec.Storage.EmptyDir == nil {
		if len(r.Spec.Storage.VolumeClaimTemplate.Spec.AccessModes) == 0 {
			r.Spec.Storage.VolumeClaimTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		}
		storage := r.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()
		if storage == nil || storage.IsZero() {
			r.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("4Gi"),
			}
		}
	}
}

//+kubebuilder:webhook:path=/validate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=vetcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &EtcdCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateCreate() (admission.Warnings, error) {
	etcdclusterlog.Info("validate create", "name", r.Name)
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	etcdclusterlog.Info("validate update", "name", r.Name)
	var warnings admission.Warnings
	oldCluster := old.(*EtcdCluster)
	if *oldCluster.Spec.Replicas != *r.Spec.Replicas {
		warnings = append(warnings, "cluster resize is not currently supported")
	}

	var allErrors field.ErrorList
	if oldCluster.Spec.Storage.EmptyDir != r.Spec.Storage.EmptyDir {
		allErrors = append(allErrors, field.Invalid(
			field.NewPath("spec", "storage", "emptyDir"),
			r.Spec.Storage.EmptyDir,
			"field is immutable"),
		)
	}

	if len(allErrors) > 0 {
		err := errors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "EtcdCluster"},
			r.Name, allErrors)
		return warnings, err
	}

	return warnings, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateDelete() (admission.Warnings, error) {
	etcdclusterlog.Info("validate delete", "name", r.Name)
	return nil, nil
}
