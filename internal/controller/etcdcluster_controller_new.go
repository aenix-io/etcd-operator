package controller

import (
	"context"
	"fmt"
	"sync"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller/factory"
	"github.com/aenix-io/etcd-operator/internal/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	ClusterDomain string
}

// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=etcd.aenix.io,resources=etcdclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups="policy",resources=poddisruptionbudgets,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;patch;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch

// Reconcile checks CR and current cluster state and performs actions to transform current state to desired.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	log.Info(ctx, "Reconciling object")
	cluster := &etcdaenixiov1alpha1.EtcdCluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if errors.IsNotFound(err) {
		log.Info(ctx, "resource not found")
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, err
	}
	state := &observables{instance: cluster}
	return r.reconcile(ctx, state)
}

// reconcile performs reconciliation of the cluster.
func (r *ClusterReconciler) reconcile(ctx context.Context, state *observables) (reconcile.Result, error) {
	if !state.instance.DeletionTimestamp.IsZero() {
		log.Debug(ctx, "resource is being deleted")
		return reconcile.Result{}, nil
	}
	if err := r.ensureUnconditionalObjects(ctx, state.instance); err != nil {
		return reconcile.Result{}, err
	}
	clusterClient, singleClients, err := r.etcdClientSet(ctx, state.instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	state.clusterClient = clusterClient
	state.singleClients = singleClients

	// checking whether any endpoints exist.
	if !state.endpointsFound() {
		// no endpoints found: right branch in flowchart
		return r.reconcileEndpointsAbsent(ctx, state)
	}
	// endpoints found: left branch in flowchart
	return r.reconcileEndpointsPresent(ctx, state)
}

// reconcileEndpointsAbsent is called in case there are no endpoints observed.
// It checks if statefulset exists and if not creates it.
func (r *ClusterReconciler) reconcileEndpointsAbsent(ctx context.Context, state *observables) (reconcile.Result, error) {
	err := r.Get(ctx, client.ObjectKeyFromObject(state.instance), &state.statefulSet)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	if !state.statefulSetExists() {
		return reconcile.Result{}, r.createClusterFromScratch(ctx, state) // todo: not implemented yet
	}
	if !state.statefulSetPodSpecCorrect() { // todo: not implemented yet
		return reconcile.Result{}, r.patchStatefulSetPodSpec(ctx, state) // todo: not implemented yet
	}
	if !state.statefulSetReady() { // todo: not implemented yet
		log.Debug(ctx, "waiting etcd cluster statefulset to become ready")
		return reconcile.Result{}, nil
	}
	if !state.statefulSetReplicasIsZero() {
		log.Error(ctx, fmt.Errorf("invalid statefulset replicas with no endpoints: %d", state.statefulSet.Spec.Replicas),
			"cluster is in invalid state, dropping from reconciliation queue")
		return reconcile.Result{}, nil
	}
	if state.etcdClusterReplicasIsZero() {
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, r.scaleUpFromZero(ctx, state) // todo: not implemented yet
}

// reconcileEndpointsPresent is called in case there are endpoints observed.
func (r *ClusterReconciler) reconcileEndpointsPresent(ctx context.Context, state *observables) (reconcile.Result, error) {
	memberReached, err := r.collectEtcdStatuses(ctx, state)
	if err != nil {
		return reconcile.Result{}, err
	}

	// checking whether members are reachable
	if !memberReached {
		// no members reachable: right branch in flowchart
		return r.reconcileMembersUnreachable(ctx, state)
	}
	// at least one member reachable: left branch in flowchart
	return r.reconcileMembersReachable(ctx, state)
}

// reconcileMembersUnreachable is called in case there are endpoints observed but not all members are reachable.
func (r *ClusterReconciler) reconcileMembersUnreachable(ctx context.Context, state *observables) (reconcile.Result, error) {
	err := r.Get(ctx, client.ObjectKeyFromObject(state.instance), &state.statefulSet)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	if !state.statefulSetExists() {
		return reconcile.Result{}, r.createOrUpdateStatefulSet(ctx, state) // todo: not implemented yet
	}
	if !state.statefulSetPodSpecCorrect() { // todo: not implemented yet
		return reconcile.Result{}, r.patchStatefulSetPodSpec(ctx, state) // todo: not implemented yet
	}
	return reconcile.Result{}, nil
}

// reconcileMembersReachable is called in case there are endpoints observed and some(all) members are reachable.
func (r *ClusterReconciler) reconcileMembersReachable(ctx context.Context, state *observables) (reconcile.Result, error) {
	state.setClusterID()
	if state.inSplitbrain() {
		log.Error(ctx, fmt.Errorf("etcd cluster in splitbrain"), "etcd cluster in split-brain, dropping from reconciliation queue")
		baseEtcdCluster := state.instance.DeepCopy()
		meta.SetStatusCondition(
			&state.instance.Status.Conditions,
			metav1.Condition{
				Type:    etcdaenixiov1alpha1.EtcdConditionError,
				Status:  metav1.ConditionTrue,
				Reason:  string(etcdaenixiov1alpha1.EtcdCondTypeSplitbrain),
				Message: string(etcdaenixiov1alpha1.EtcdErrorCondSplitbrainMessage),
			},
		)
		return reconcile.Result{}, r.Status().Patch(ctx, state.instance, client.MergeFrom(baseEtcdCluster))
	}
	if !state.clusterHasQuorum() {
		log.Error(ctx, fmt.Errorf("cluster has lost quorum"), "cluster has lost quorum, dropping from reconciliation queue")
		return reconcile.Result{}, nil
	}
	if !state.allMembersAreManaged() { // todo: not implemented yet
		log.Error(ctx, fmt.Errorf("not all members are managed"), "not all members are managed, dropping from reconciliation queue")
		return reconcile.Result{}, nil
	}
	if state.hasLearners() {
		return reconcile.Result{}, r.promoteLearners(ctx, state) // todo: not implemented yet
	}
	if !state.allMembersAreHealthy() { // todo: not implemented yet
		// todo: enqueue unhealthy member(s) eviction
		//  then delete pod, pvc & update config map respectively
		return reconcile.Result{}, nil
	}
	if err := r.createOrUpdateClusterStateConfigMap(ctx, state); err != nil { // todo: not implemented yet
		return reconcile.Result{}, err
	}
	if !state.statefulSetPodSpecCorrect() { // todo: not implemented yet
		return reconcile.Result{}, r.patchStatefulSetPodSpec(ctx, state) // todo: not implemented yet
	}
	if !state.podsPresentInMembersList() { // todo: not implemented yet
		// todo: delete pod, pvc & update config map respectively
		return reconcile.Result{}, nil
	}
	return r.reconcileReplicas(ctx, state)
}

// reconcileReplicas is called in case there are endpoints observed and all members are reachable,
// healthy and present in members list.
func (r *ClusterReconciler) reconcileReplicas(ctx context.Context, state *observables) (reconcile.Result, error) {
	if *state.instance.Spec.Replicas == 0 && *state.statefulSet.Spec.Replicas == 1 {
		return reconcile.Result{}, r.scaleDownToZero(ctx, state) // todo: not implemented yet
	}
	if *state.instance.Spec.Replicas < *state.statefulSet.Spec.Replicas {
		return reconcile.Result{}, r.scaleDown(ctx, state) // todo: not implemented yet
	}
	if *state.instance.Spec.Replicas > *state.statefulSet.Spec.Replicas {
		return reconcile.Result{}, r.scaleUp(ctx, state) // todo: not implemented yet
	}

	baseEtcdCluster := state.instance.DeepCopy()
	meta.SetStatusCondition(
		&state.instance.Status.Conditions,
		metav1.Condition{
			Type:   etcdaenixiov1alpha1.EtcdConditionReady,
			Status: metav1.ConditionTrue,
			Reason: string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete),
		},
	)

	return reconcile.Result{}, r.Status().Patch(ctx, state.instance, client.MergeFrom(baseEtcdCluster))
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdaenixiov1alpha1.EtcdCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Complete(r)
}

// ensureUnconditionalObjects ensures that objects that should always exist are created.
func (r *ClusterReconciler) ensureUnconditionalObjects(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) error {
	g, ctx := errgroup.WithContext(ctx)
	wrapWithMessage := func(err error, msg string) error {
		if err != nil {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return nil
	}
	g.Go(func() error {
		return wrapWithMessage(factory.CreateOrUpdateClientService(ctx, cluster, r.Client), "failed to ensure client service")
	})
	g.Go(func() error {
		return wrapWithMessage(factory.CreateOrUpdateHeadlessService(ctx, cluster, r.Client), "failed to ensure headless service")
	})
	g.Go(func() error {
		return wrapWithMessage(factory.CreateOrUpdatePdb(ctx, cluster, r.Client), "failed to ensure pod disruption budget")
	})
	return g.Wait()
}

// etcdClientSet returns etcd client set for given cluster.
func (r *ClusterReconciler) etcdClientSet(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) (*clientv3.Client, []*clientv3.Client, error) {
	cfg, err := r.etcdClusterConfig(ctx, cluster)
	if err != nil {
		return nil, nil, err
	}
	if len(cfg.Endpoints) == 0 {
		return nil, nil, nil
	}
	eps := cfg.Endpoints
	clusterClient, err := clientv3.New(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("error building etcd cluster client: %w", err)
	}
	membersClients := make([]*clientv3.Client, len(eps))
	for i, ep := range eps {
		cfg.Endpoints = []string{ep}
		membersClients[i], err = clientv3.New(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("error building etcd single-endpoint client for endpoint %s: %w", ep, err)
		}
	}
	return clusterClient, membersClients, nil
}

// collectEtcdStatuses collects etcd members statuses for given cluster.
func (r *ClusterReconciler) collectEtcdStatuses(ctx context.Context, state *observables) (bool, error) {
	state.etcdStatuses = make([]etcdStatus, len(state.singleClients))
	{
		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(ctx, etcdDefaultTimeout)
		for i := range state.singleClients {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				state.etcdStatuses[i].fill(ctx, state.singleClients[i])
			}(i)
		}
		wg.Wait()
		cancel()
	}

	memberReached := false
	for i := range state.etcdStatuses {
		if state.etcdStatuses[i].endpointStatus != nil {
			memberReached = true
			break
		}
	}
	return memberReached, nil
}

// etcdClusterConfig returns etcd client config for given cluster.
func (r *ClusterReconciler) etcdClusterConfig(ctx context.Context, cluster *etcdaenixiov1alpha1.EtcdCluster) (clientv3.Config, error) {
	ep := corev1.Endpoints{}
	err := r.Get(ctx, types.NamespacedName{Name: factory.GetHeadlessServiceName(cluster), Namespace: cluster.Namespace}, &ep)
	if client.IgnoreNotFound(err) != nil {
		return clientv3.Config{}, err
	}
	if err != nil {
		return clientv3.Config{Endpoints: []string{}}, nil
	}

	names := map[string]struct{}{}
	urls := make([]string, 0, 8)
	for _, v := range ep.Subsets {
		for _, addr := range v.Addresses {
			names[addr.Hostname] = struct{}{}
		}
		for _, addr := range v.NotReadyAddresses {
			names[addr.Hostname] = struct{}{}
		}
	}
	for name := range names {
		urls = append(urls, fmt.Sprintf("%s.%s.%s.svc.%s:%s", name, ep.Name, cluster.Namespace, r.ClusterDomain, "2379"))
	}

	return clientv3.Config{Endpoints: urls}, nil
}

// todo: implement this
func (r *ClusterReconciler) createClusterFromScratch(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) patchStatefulSetPodSpec(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) scaleUp(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) scaleUpFromZero(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) scaleDown(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) scaleDownToZero(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) createOrUpdateStatefulSet(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) promoteLearners(ctx context.Context, state *observables) error {
	panic("not implemented")
}

// todo: implement this
func (r *ClusterReconciler) createOrUpdateClusterStateConfigMap(ctx context.Context, state *observables) error {
	panic("not implemented")
}
