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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/log"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/k8sutils"
)

func GetServiceName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if cluster.Spec.ServiceTemplate != nil && cluster.Spec.ServiceTemplate.Name != "" {
		return cluster.Spec.ServiceTemplate.Name
	}

	return cluster.Name
}

func GetHeadlessServiceName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if cluster.Spec.HeadlessServiceTemplate != nil && cluster.Spec.HeadlessServiceTemplate.Name != "" {
		return cluster.Spec.HeadlessServiceTemplate.Name
	}

	return fmt.Sprintf("%s-headless", cluster.Name)
}

func CreateOrUpdateHeadlessService(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	logger := log.FromContext(ctx)
	var err error

	metadata := metav1.ObjectMeta{
		Name:      GetHeadlessServiceName(cluster),
		Namespace: cluster.Namespace,
		Labels:    NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
	}

	if cluster.Spec.HeadlessServiceTemplate != nil {
		metadata, err = k8sutils.StrategicMerge(metadata, cluster.Spec.HeadlessServiceTemplate.ToObjectMeta())
		if err != nil {
			return fmt.Errorf("cannot strategic-merge base svc metadata with headlessServiceTemplate.metadata: %w", err)
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metadata,
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "peer", TargetPort: intstr.FromInt32(2380), Port: 2380, Protocol: corev1.ProtocolTCP},
				{Name: "client", TargetPort: intstr.FromInt32(2379), Port: 2379, Protocol: corev1.ProtocolTCP},
			},
			Type:                     corev1.ServiceTypeClusterIP,
			ClusterIP:                "None",
			Selector:                 NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
			PublishNotReadyAddresses: true,
		},
	}

	logger.V(2).Info("cluster service spec generated", "svc_name", svc.Name, "svc_spec", svc.Spec)

	if err := ctrl.SetControllerReference(cluster, svc, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileService(ctx, rclient, cluster.Name, svc)
}

func CreateOrUpdateClientService(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	logger := log.FromContext(ctx)
	var err error

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "client", TargetPort: intstr.FromInt32(2379), Port: 2379, Protocol: corev1.ProtocolTCP},
			},
			Type:     corev1.ServiceTypeClusterIP,
			Selector: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
		},
	}

	if cluster.Spec.ServiceTemplate != nil {
		svc, err = k8sutils.StrategicMerge(svc, corev1.Service{
			ObjectMeta: cluster.Spec.ServiceTemplate.EmbeddedObjectMetadata.ToObjectMeta(),
			Spec:       cluster.Spec.ServiceTemplate.Spec,
		})
		if err != nil {
			return fmt.Errorf("cannot strategic-merge base svc with serviceTemplate: %w", err)
		}
	}

	logger.V(2).Info("client service spec generated", "svc_name", svc.Name, "svc_spec", svc.Spec)

	if err := ctrl.SetControllerReference(cluster, &svc, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileService(ctx, rclient, cluster.Name, &svc)
}
