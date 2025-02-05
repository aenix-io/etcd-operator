package controller

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/pkg/set"
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

// TODO: nolint
// observables stores observations that the operator can make about
// states of objects in kubernetes
type observables struct {
	instance      *v1alpha1.EtcdCluster
	statefulSet   appsv1.StatefulSet
	etcdStatuses  []etcdStatus
	clusterID     uint64
	pvcs          []corev1.PersistentVolumeClaim //nolint:unused
	clusterClient *clientv3.Client
	singleClients []*clientv3.Client
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
// Also if members have different opinions on the list of members, this is
// also a splitbrain.
func (o *observables) inSplitbrain() bool {
	return !o.clusterIDsAllEqual() || !o.memberListsAllEqual()
}

func (o *observables) clusterIDsAllEqual() bool {
	ids := set.New[uint64]()
	for i := range o.etcdStatuses {
		if o.etcdStatuses[i].endpointStatus != nil {
			ids.Add(o.etcdStatuses[i].endpointStatus.Header.ClusterId)
		}
	}
	return len(ids) <= 1
}

func (o *observables) memberListsAllEqual() bool {
	type m struct {
		Name string
		ID   uint64
	}
	memberLists := make([]set.Set[m], 0, len(o.etcdStatuses))
	for i := range o.etcdStatuses {
		if o.etcdStatuses[i].memberList != nil {
			memberSet := set.New[m]()
			for _, member := range o.etcdStatuses[i].memberList.Members {
				memberSet.Add(m{member.Name, member.ID})
			}
			memberLists = append(memberLists, memberSet)
		}
	}
	for i := range memberLists {
		if !memberLists[0].Equals(memberLists[i]) {
			return false
		}
	}
	return true
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

func (o *observables) pvcMaxIndex() (max int) {
	max = -1
	for i := range o.pvcs {
		tokens := strings.Split(o.pvcs[i].Name, "-")
		index, err := strconv.Atoi(tokens[len(tokens)-1])
		if err != nil {
			continue
		}
		if index > max {
			max = index
		}
	}
	return max
}

func (o *observables) endpointMaxIndex() (max int) {
	for i := range o.endpoints() {
		tokens := strings.Split(o.endpoints()[i], ":")
		if len(tokens) < 2 {
			continue
		}
		tokens = strings.Split(tokens[len(tokens)-2], "-")
		index, err := strconv.Atoi(tokens[len(tokens)-1])
		if err != nil {
			continue
		}
		if index > max {
			max = index
		}
	}
	return max
}

// TODO: make a real function to determine the right number of replicas.
// Hint: if ClientURL in the member list is absent, the member has not yet
// started, but if the name field is populated, this is a member of the
// initial cluster. If the name field is empty, this member has just been
// added with etcdctl member add (or equivalent API call).
// nolint:unused
func (o *observables) desiredReplicas() (max int) {
	max = -1
	if o.etcdStatuses != nil {
		for i := range o.etcdStatuses {
			if o.etcdStatuses[i].memberList != nil {
				for j := range o.etcdStatuses[i].memberList.Members {
					tokens := strings.Split(o.etcdStatuses[i].memberList.Members[j].Name, "-")
					index, err := strconv.Atoi(tokens[len(tokens)-1])
					if err != nil {
						continue
					}
					if index > max {
						max = index
					}
				}
			}
		}
	}
	if max > -1 {
		return max + 1
	}

	if epMax := o.endpointMaxIndex(); epMax > max {
		max = epMax
	}
	if pvcMax := o.pvcMaxIndex(); pvcMax > max {
		max = pvcMax
	}
	if max == -1 {
		return int(*o.instance.Spec.Replicas)
	}
	return max + 1
}

func (o *observables) statefulSetExists() bool {
	return o.statefulSet.UID != ""
}

func (o *observables) endpoints() []string {
	return o.clusterClient.Endpoints()
}

func (o *observables) endpointsFound() bool {
	return o.clusterClient != nil && o.singleClients != nil
}

// TODO: compare the desired sts with what exists
func (o *observables) statefulSetPodSpecCorrect() bool {
	return true
}

// TODO: also use updated replicas field?
func (o *observables) statefulSetReady() bool {
	return o.statefulSet.Status.ReadyReplicas == *o.statefulSet.Spec.Replicas
}

func (o *observables) statefulSetReplicasIsZero() bool {
	return *o.statefulSet.Spec.Replicas == 0
}

func (o *observables) etcdClusterReplicasIsZero() bool {
	return *o.instance.Spec.Replicas == 0
}

func (o *observables) clusterHasQuorum() bool {
	size := len(o.etcdStatuses)
	membersInQuorum := size
	for i := range o.etcdStatuses {
		if o.etcdStatuses[i].endpointStatus == nil || o.etcdStatuses[i].endpointStatus.Leader == 0 {
			membersInQuorum--
		}
	}
	return membersInQuorum*2 > size
}

func (o *observables) hasLearners() bool {
	for i := range o.etcdStatuses {
		if stat := o.etcdStatuses[i].endpointStatus; stat != nil && stat.IsLearner {
			return true
		}
	}
	return false
}

// TODO: check if the pods are in the member list
func (o *observables) podsPresentInMembersList() bool {
	return true
}

// TODO: check whether all members are healthy
func (o *observables) allMembersAreHealthy() bool {
	return true
}

// TODO: check whether all members are managed
func (o *observables) allMembersAreManaged() bool {
	return true
}
