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

// +kubebuilder:webhook:path=/mutate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=metcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &EtcdCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *EtcdCluster) Default() {
	etcdclusterlog.Info("default", "name", r.Name)
	if len(r.Spec.PodSpec.Image) == 0 {
		r.Spec.PodSpec.Image = defaultEtcdImage
	}
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

// +kubebuilder:webhook:path=/validate-etcd-aenix-io-v1alpha1-etcdcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=etcd.aenix.io,resources=etcdclusters,verbs=create;update,versions=v1alpha1,name=vetcdcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &EtcdCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *EtcdCluster) ValidateCreate() (admission.Warnings, error) {
	etcdclusterlog.Info("validate create", "name", r.Name)
	warnings, err := r.validatePdb()
	if err != nil {
		return nil, errors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "EtcdCluster"},
			r.Name, err)
	}

	return warnings, nil
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
	if oldCluster.Spec.Storage.EmptyDir == nil && r.Spec.Storage.EmptyDir != nil ||
		oldCluster.Spec.Storage.EmptyDir != nil && r.Spec.Storage.EmptyDir == nil {
		allErrors = append(allErrors, field.Invalid(
			field.NewPath("spec", "storage", "emptyDir"),
			r.Spec.Storage.EmptyDir,
			"field is immutable"),
		)
	}

	pdbWarnings, pdbErr := r.validatePdb()
	if pdbErr != nil {
		allErrors = append(allErrors, pdbErr...)
	}
	if len(pdbWarnings) > 0 {
		warnings = append(warnings, pdbWarnings...)
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

// validatePdb validates PDB fields
func (r *EtcdCluster) validatePdb() (admission.Warnings, field.ErrorList) {
	if !r.Spec.PodDisruptionBudget.Enabled {
		return nil, nil
	}
	pdb := r.Spec.PodDisruptionBudget
	var warnings admission.Warnings
	var allErrors field.ErrorList
	if pdb.Spec.MinAvailable.IntValue() != 0 && pdb.Spec.MaxUnavailable.IntValue() != 0 {
		allErrors = append(allErrors, field.Invalid(
			field.NewPath("spec", "podDisruptionBudget", "minAvailable"),
			pdb.Spec.MinAvailable.IntValue(),
			"minAvailable is mutually exclusive with maxUnavailable"),
		)
	}
	minQuorumSize := r.calculateQuorumSize()
	if pdb.Spec.MinAvailable.IntValue() != 0 {
		if pdb.Spec.MinAvailable.IntValue() < 0 {
			allErrors = append(allErrors, field.Invalid(
				field.NewPath("spec", "podDisruptionBudget", "minAvailable"),
				pdb.Spec.MinAvailable.IntValue(),
				"value cannot be less than zero"),
			)
		}
		if pdb.Spec.MinAvailable.IntValue() > int(*r.Spec.Replicas) {
			allErrors = append(allErrors, field.Invalid(
				field.NewPath("spec", "podDisruptionBudget", "minAvailable"),
				pdb.Spec.MinAvailable.IntValue(),
				"value cannot be larger than number of replicas"),
			)
		}
		if pdb.Spec.MinAvailable.IntValue() < minQuorumSize {
			warnings = append(warnings, "current number of spec.podDisruptionBudget.minAvailable can lead to loss of quorum")
		}
	}
	if pdb.Spec.MaxUnavailable.IntValue() != 0 {
		if pdb.Spec.MaxUnavailable.IntValue() < 0 {
			allErrors = append(allErrors, field.Invalid(
				field.NewPath("spec", "podDisruptionBudget", "maxUnavailable"),
				pdb.Spec.MaxUnavailable.IntValue(),
				"value cannot be less than zero"),
			)
		}
		if pdb.Spec.MaxUnavailable.IntValue() > int(*r.Spec.Replicas) {
			allErrors = append(allErrors, field.Invalid(
				field.NewPath("spec", "podDisruptionBudget", "maxUnavailable"),
				pdb.Spec.MaxUnavailable.IntValue(),
				"value cannot be larger than number of replicas"),
			)
		}
		if int(*r.Spec.Replicas)-pdb.Spec.MaxUnavailable.IntValue() < minQuorumSize {
			warnings = append(warnings, "current number of spec.podDisruptionBudget.maxUnavailable can lead to loss of quorum")
		}
	}

	if len(allErrors) > 0 {
		return nil, allErrors
	}

	return warnings, nil
}
