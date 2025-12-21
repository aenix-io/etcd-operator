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
	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

func PVCLabels(cluster *etcdaenixiov1alpha1.EtcdCluster) map[string]string {
	labels := PodLabels(cluster)
	for key, value := range cluster.Spec.Storage.VolumeClaimTemplate.Labels {
		labels[key] = value
	}
	return labels
}

func GetPVCName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if len(cluster.Spec.Storage.VolumeClaimTemplate.Name) > 0 {
		return cluster.Spec.Storage.VolumeClaimTemplate.Name
	}
	//nolint:goconst
	return "data"
}
