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
