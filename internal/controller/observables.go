package controller

import (
	"context"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// etcdStatus holds the details of the status that an etcd endpoint
// can return about itself, i.e. its own status and its perceived
// member list
type etcdStatus struct {
	endpointStatus      *clientv3.StatusResponse
	endpointStatusError error
	memberList          *clientv3.MemberListResponse
	memberListError     error
}

// observables stores observations that the operator can make about
// states of objects in kubernetes
type observables struct {
	statefulSet    appsv1.StatefulSet
	stsExists      bool
	endpointsFound bool
	etcdStatuses   []etcdStatus
	clusterID      uint64
	_              int
	_              []corev1.PersistentVolumeClaim
}

// setClusterID populates the clusterID field based on etcdStatuses
func (o *observables) setClusterID() {
	for i := range o.etcdStatuses {
		if o.etcdStatuses[i].endpointStatus != nil {
			o.clusterID = o.etcdStatuses[i].endpointStatus.Header.ClusterId
			return
		}
	}
}

// inSplitbrain compares clusterID field with clusterIDs in etcdStatuses.
// If more than one unique ID is reported, cluster is in splitbrain.
func (o *observables) inSplitbrain() bool {
	for i := range o.etcdStatuses {
		if o.etcdStatuses[i].endpointStatus != nil {
			if o.clusterID != o.etcdStatuses[i].endpointStatus.Header.ClusterId {
				return true
			}
		}
	}
	return false
}

// fill takes a single-endpoint client and populates the fields of etcdStatus
// with the endpoint's status and its perceived member list.
func (s *etcdStatus) fill(ctx context.Context, c *clientv3.Client) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.endpointStatus, s.endpointStatusError = c.Status(ctx, c.Endpoints()[0])
	}()
	s.memberList, s.memberListError = c.MemberList(ctx)
	wg.Wait()
}

// TODO: make a real function
func (o *observables) _() int {
	if o.etcdStatuses != nil {
		for i := range o.etcdStatuses {
			if o.etcdStatuses[i].memberList != nil {
				return len(o.etcdStatuses[i].memberList.Members)
			}
		}
	}
	return 0
}
