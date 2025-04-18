package controller

type State interface {
	ClusterExists() bool
	PendingDeletion() bool
	GetEtcdCluster() error
}
