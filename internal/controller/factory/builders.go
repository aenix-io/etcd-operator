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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func reconcileOwnedResource(ctx context.Context, c client.Client, resource client.Object) error {
	gvk, err := apiutil.GVKForObject(resource, c.Scheme())
	if err != nil {
		return err
	}
	logger := log.FromContext(ctx).WithValues("group", gvk.GroupVersion().String(), "kind", gvk.Kind, "name", resource.GetName())
	logger.V(2).Info("reconciling owned resource")

	base := resource.DeepCopyObject().(client.Object)
	err = c.Get(ctx, client.ObjectKeyFromObject(resource), base)
	if err == nil {
		logger.V(2).Info("updating owned resource")
		resource.SetAnnotations(labels.Merge(base.GetAnnotations(), resource.GetAnnotations()))
		resource.SetResourceVersion(base.GetResourceVersion())
		logger.V(2).Info("owned resource annotations merged", "annotations", resource.GetAnnotations())
		return c.Update(ctx, resource)
	}
	if errors.IsNotFound(err) {
		logger.V(2).Info("creating new owned resource")
		return c.Create(ctx, resource)
	}
	return fmt.Errorf("error getting owned resource: %w", err)
}

func deleteOwnedResource(ctx context.Context, c client.Client, resource client.Object) error {
	gvk, err := apiutil.GVKForObject(resource, c.Scheme())
	if err != nil {
		return err
	}
	logger := log.FromContext(ctx).WithValues("group", gvk.GroupVersion().String(), "kind", gvk.Kind, "name", resource.GetName())
	logger.V(2).Info("deleting owned resource")
	return client.IgnoreNotFound(c.Delete(ctx, resource))
}
