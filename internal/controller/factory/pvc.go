package factory

import etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"

func GetPVCName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	if len(cluster.Spec.Storage.VolumeClaimTemplate.Name) > 0 {
		return cluster.Spec.Storage.VolumeClaimTemplate.Name
	}

	return "data"
}
