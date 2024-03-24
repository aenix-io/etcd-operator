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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func reconcileSTS(ctx context.Context, rclient client.Client, crdName string, sts *appsv1.StatefulSet) error {
	logger := log.FromContext(ctx)

	currentSts := &appsv1.StatefulSet{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(2).Info("creating new statefulset", "sts_name", sts.Name, "crd_object", crdName)
			return rclient.Create(ctx, sts)
		}
		return fmt.Errorf("cannot get existing statefulset: %s, for crd_object: %s, err: %w", sts.Name, crdName, err)
	}
	sts.Annotations = labels.Merge(sts.Annotations, sts.Annotations)
	if sts.ResourceVersion != "" {
		sts.ResourceVersion = currentSts.ResourceVersion
	}
	sts.Status = currentSts.Status
	return rclient.Update(ctx, sts)
}
