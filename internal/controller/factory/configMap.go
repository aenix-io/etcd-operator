package factory

import etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"

func GetClusterStateConfigMapName(cluster *etcdaenixiov1alpha1.EtcdCluster) string {
	return cluster.Name + "-cluster-state"
}
