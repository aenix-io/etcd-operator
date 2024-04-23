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
	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func reconcileStatefulSet(ctx context.Context, rclient client.Client, crdName string, sts *appsv1.StatefulSet) error {
	logger := log.FromContext(ctx)
	logger.V(2).Info("statefulset reconciliation started")

	currentSts := &appsv1.StatefulSet{}
	logger.V(2).Info("statefulset found", "sts_name", currentSts.Name)

	err := rclient.Get(ctx, types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("creating new statefulset", "sts_name", sts.Name, "crd_object", crdName)
			return rclient.Create(ctx, sts)
		}
		return fmt.Errorf("cannot get existing statefulset: %s, for crd_object: %s, err: %w", sts.Name, crdName, err)
	}
	sts.Annotations = labels.Merge(currentSts.Annotations, sts.Annotations)
	logger.V(2).Info("statefulset annotations merged", "sts_annotations", sts.Annotations)
	sts.Status = currentSts.Status
	return rclient.Update(ctx, sts)
}

func reconcileConfigMap(ctx context.Context, rclient client.Client, crdName string, configMap *corev1.ConfigMap) error {
	logger := log.FromContext(ctx)
	logger.V(2).Info("configmap reconciliation started")

	currentConfigMap := &corev1.ConfigMap{}
	logger.V(2).Info("configmap found", "cm_name", currentConfigMap.Name)

	err := rclient.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, currentConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("creating new configMap", "cm_name", configMap.Name, "crd_object", crdName)
			return rclient.Create(ctx, configMap)
		}
		return fmt.Errorf("cannot get existing configMap: %s, for crd_object: %s, err: %w", configMap.Name, crdName, err)
	}
	configMap.Annotations = labels.Merge(currentConfigMap.Annotations, configMap.Annotations)
	logger.V(2).Info("configmap annotations merged", "cm_annotations", configMap.Annotations)
	return rclient.Update(ctx, configMap)
}

func reconcileService(ctx context.Context, rclient client.Client, crdName string, svc *corev1.Service) error {
	logger := log.FromContext(ctx)
	logger.V(2).Info("service reconciliation started")

	if svc == nil {
		return fmt.Errorf("service is nil for crd_object: %s", crdName)
	}

	currentSvc := &corev1.Service{}
	logger.V(2).Info("service found", "svc_name", currentSvc.Name)

	err := rclient.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, currentSvc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("creating new service", "svc_name", svc.Name, "crd_object", crdName)
			return rclient.Create(ctx, svc)
		}
		return fmt.Errorf("cannot get existing service: %s, for crd_object: %s, err: %w", svc.Name, crdName, err)
	}
	svc.Annotations = labels.Merge(currentSvc.Annotations, svc.Annotations)
	logger.V(2).Info("service annotations merged", "svc_annotations", svc.Annotations)
	svc.Status = currentSvc.Status
	return rclient.Update(ctx, svc)
}

// deleteManagedPdb deletes cluster PDB if it exists.
// pdb parameter should have at least metadata.name and metadata.namespace fields filled.
func deleteManagedPdb(ctx context.Context, rclient client.Client, pdb *v1.PodDisruptionBudget) error {
	logger := log.FromContext(ctx)

	currentPdb := &v1.PodDisruptionBudget{}
	logger.V(2).Info("pdb found", "pdb_name", currentPdb.Name)

	err := rclient.Get(ctx, types.NamespacedName{Namespace: pdb.Namespace, Name: pdb.Name}, currentPdb)
	if err != nil {
		logger.V(2).Info("error getting cluster PDB", "error", err)
		return client.IgnoreNotFound(err)
	}
	err = rclient.Delete(ctx, currentPdb)
	if err != nil {
		logger.Error(err, "error deleting cluster PDB", "name", pdb.Name)
		return client.IgnoreNotFound(err)
	}

	return nil
}

func reconcilePdb(ctx context.Context, rclient client.Client, crdName string, pdb *v1.PodDisruptionBudget) error {
	logger := log.FromContext(ctx)
	logger.V(2).Info("pdb reconciliation started")

	currentPdb := &v1.PodDisruptionBudget{}
	logger.V(2).Info("pdb found", "pdb_name", currentPdb.Name)

	err := rclient.Get(ctx, types.NamespacedName{Namespace: pdb.Namespace, Name: pdb.Name}, currentPdb)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("creating new PDB", "pdb_name", pdb.Name, "crd_object", crdName)
			return rclient.Create(ctx, pdb)
		}
		logger.V(2).Info("error getting cluster PDB", "error", err)
		return fmt.Errorf("cannot get existing pdb resource: %s for crd_object: %s, err: %w", pdb.Name, crdName, err)
	}
	pdb.Annotations = labels.Merge(currentPdb.Annotations, pdb.Annotations)
	logger.V(2).Info("pdb annotations merged", "pdb_annotations", pdb.Annotations)
	pdb.ResourceVersion = currentPdb.ResourceVersion
	pdb.Status = currentPdb.Status
	return rclient.Update(ctx, pdb)
}
