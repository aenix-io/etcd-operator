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

package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	goerrors "errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/etcd-operator/internal/log"
	policyv1 "k8s.io/api/policy/v1"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller/factory"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	etcdDefaultTimeout = 5 * time.Second
)

// EtcdClusterReconciler reconciles a EtcdCluster object
type EtcdClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
func (r *EtcdClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Debug(ctx, "reconciling object")
	state := &observables{}
	state.instance = &etcdaenixiov1alpha1.EtcdCluster{}
	err := r.Get(ctx, req.NamespacedName, state.instance)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Debug(ctx, "object not found")
			return ctrl.Result{}, nil
		}
		// Error retrieving object, requeue
		return reconcile.Result{}, err
	}
	// If object is being deleted, skipping reconciliation
	if !state.instance.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	// create two services and the pdb
	err = r.ensureUnconditionalObjects(ctx, state)
	if err != nil {
		return ctrl.Result{}, err
	}

	// fetch STS if exists
	err = r.Get(ctx, req.NamespacedName, &state.statefulSet)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("couldn't get statefulset: %w", err)
	}
	// state.stsExists = state.statefulSet.UID != ""

	// fetch endpoints
	clusterClient, singleClients, err := factory.NewEtcdClientSet(ctx, state.instance, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// state.endpointsFound = clusterClient != nil && singleClients != nil

	// if clusterClient != nil {
	// 	state.endpoints = clusterClient.Endpoints()
	// }
	state.clusterClient = clusterClient
	state.singleClients = singleClients

	// fetch PVCs
	state.pvcs, err = factory.PVCs(ctx, state.instance, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !state.endpointsFound() {
		if !state.statefulSetExists() {
			return r.createClusterFromScratch(ctx, state) // TODO: needs implementing
		}

		// update sts pod template (and only pod template) if it doesn't match desired state
		if !state.statefulSetPodSpecCorrect() { // TODO: needs implementing
			desiredSts := factory.TemplateStatefulSet() // TODO: needs implementing
			state.statefulSet.Spec.Template.Spec = desiredSts.Spec.Template.Spec
			return ctrl.Result{}, r.patchOrCreateObject(ctx, &state.statefulSet)
		}

		if !state.statefulSetReady() { // TODO: needs improved implementation?
			return ctrl.Result{}, fmt.Errorf("waiting for statefulset to become ready")
		}

		if *state.statefulSet.Spec.Replicas > 0 {
			return ctrl.Result{}, fmt.Errorf("reached an impossible state (no endpoints, but active pods)")
		}

		if *state.instance.Spec.Replicas == 0 {
			// cluster successfully scaled down to zero
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, r.scaleUpFromZero(ctx) // TODO: needs implementing
	}

	// get status of every endpoint and member list from every endpoint
	state.etcdStatuses = make([]etcdStatus, len(singleClients))
	{
		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(ctx, etcdDefaultTimeout)
		for i := range singleClients {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				state.etcdStatuses[i].fill(ctx, singleClients[i])
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

	if !memberReached {
		return ctrl.Result{}, r.createOrUpdateStatefulSet(ctx)
	}

	state.setClusterID()
	if state.inSplitbrain() {
		log.Error(ctx, fmt.Errorf("etcd cluster in splitbrain"), "etcd cluster in splitbrain, dropping from reconciliation queue")
		meta.SetStatusCondition(
			&state.instance.Status.Conditions,
			metav1.Condition{
				Type:    etcdaenixiov1alpha1.EtcdConditionError,
				Status:  metav1.ConditionTrue,
				Reason:  string(etcdaenixiov1alpha1.EtcdCondTypeSplitbrain),
				Message: string(etcdaenixiov1alpha1.EtcdErrorCondSplitbrainMessage),
			},
		)
		return r.updateStatus(ctx, state)
	}

	if !state.clusterHasQuorum() {
		// we can't do anything about this but we still return an error to check on the cluster from time to time
		return ctrl.Result{}, fmt.Errorf("cluster has lost quorum")
	}

	if state.hasLearners() {
		return ctrl.Result{}, r.promoteLearners(ctx)
	}

	if err := r.createOrUpdateClusterStateConfigMap(ctx); err != nil {
		return ctrl.Result{}, err
	}

	if !state.statefulSetPodSpecCorrect() {
		return ctrl.Result{}, r.createOrUpdateStatefulSet(ctx)
	}

	// if size is different we have to remove statefulset it will be recreated in the next step
	if err := r.checkAndDeleteStatefulSetIfNecessary(ctx, state); err != nil {
		return ctrl.Result{}, err
	}

	/* Saved as an example
	// set cluster initialization condition
	meta.SetStatusCondition(
		&state.instance.Status.Conditions,
		metav1.Condition{
			Type:    etcdaenixiov1alpha1.EtcdConditionInitialized,
			Status:  metav1.ConditionTrue,
			Reason:  string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete),
			Message: string(etcdaenixiov1alpha1.EtcdInitCondPosMessage),
		},
	)
	*/
	return r.updateStatus(ctx, state)
}

// checkAndDeleteStatefulSetIfNecessary deletes the StatefulSet if the specified storage size has changed.
func (r *EtcdClusterReconciler) checkAndDeleteStatefulSetIfNecessary(ctx context.Context, state *observables) error {
	for _, volumeClaimTemplate := range state.statefulSet.Spec.VolumeClaimTemplates {
		if volumeClaimTemplate.Name != "data" {
			continue
		}
		currentStorage := volumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]
		desiredStorage := state.instance.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]
		if desiredStorage.Cmp(currentStorage) != 0 {
			deletePolicy := metav1.DeletePropagationOrphan
			log.Info(ctx, "Deleting StatefulSet due to storage change", "statefulSet", state.statefulSet.Name)
			err := r.Delete(ctx, &state.statefulSet, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
			if err != nil {
				log.Error(ctx, err, "Failed to delete StatefulSet")
				return err
			}
			return nil
		}
	}
	return nil
}

// ensureConditionalClusterObjects creates or updates all objects owned by cluster CR
func (r *EtcdClusterReconciler) ensureConditionalClusterObjects(ctx context.Context, state *observables) error {

	if err := factory.CreateOrUpdateClusterStateConfigMap(ctx, state.instance, r.Client); err != nil {
		log.Error(ctx, err, "reconcile cluster state configmap failed")
		return err
	}
	log.Debug(ctx, "cluster state configmap reconciled")

	if err := factory.CreateOrUpdateStatefulSet(ctx, state.instance, r.Client); err != nil {
		log.Error(ctx, err, "reconcile statefulset failed")
		return err
	}

	if err := factory.UpdatePersistentVolumeClaims(ctx, state.instance, r.Client); err != nil {
		log.Error(ctx, err, "reconcile persistentVolumeClaims failed")
		return err
	}
	log.Debug(ctx, "statefulset reconciled")

	return nil
}

// updateStatusOnErr wraps error and updates EtcdCluster status
func (r *EtcdClusterReconciler) updateStatusOnErr(ctx context.Context, state *observables, err error) (ctrl.Result, error) {
	// The function 'updateStatusOnErr' will always return non-nil error. Hence, the ctrl.Result will always be ignored.
	// Therefore, the ctrl.Result returned by 'updateStatus' function can be discarded.
	// REF: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile@v0.17.3#Reconciler
	_, statusErr := r.updateStatus(ctx, state)
	if statusErr != nil {
		return ctrl.Result{}, goerrors.Join(statusErr, err)
	}
	return ctrl.Result{}, err
}

// updateStatus updates EtcdCluster status and returns error and requeue in case status could not be updated due to conflict
func (r *EtcdClusterReconciler) updateStatus(ctx context.Context, state *observables) (ctrl.Result, error) {
	err := r.Status().Update(ctx, state.instance)
	if err == nil {
		return ctrl.Result{}, nil
	}
	if errors.IsConflict(err) {
		log.Debug(ctx, "conflict during cluster status update")
		return ctrl.Result{Requeue: true}, nil
	}
	log.Error(ctx, err, "cannot update cluster status")
	return ctrl.Result{}, err
}

// isStatefulSetReady gets managed StatefulSet and checks its readiness.
func (r *EtcdClusterReconciler) isStatefulSetReady(ctx context.Context, state *observables) (bool, error) {
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(state.instance), sts)
	if err == nil {
		return sts.Status.ReadyReplicas == *sts.Spec.Replicas, nil
	}
	return false, client.IgnoreNotFound(err)
}

// SetupWithManager sets up the controller with the Manager.
func (r *EtcdClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdaenixiov1alpha1.EtcdCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Complete(r)
}

func (r *EtcdClusterReconciler) configureAuth(ctx context.Context, state *observables) error {

	var err error

	cli, err := r.GetEtcdClient(ctx, state.instance)
	if err != nil {
		return err
	}

	defer func() {
		err = cli.Close()
	}()

	err = testMemberList(ctx, cli)
	if err != nil {
		return err
	}

	auth := clientv3.NewAuth(cli)

	if state.instance.Spec.Security != nil && state.instance.Spec.Security.EnableAuth {

		if err := r.createRoleIfNotExists(ctx, auth, "root"); err != nil {
			return err
		}

		rootUserResponse, err := r.createUserIfNotExists(ctx, auth, "root")
		if err != nil {
			return err
		}

		if err := r.grantRoleToUser(ctx, auth, "root", "root", rootUserResponse); err != nil {
			return err
		}

		if err := r.enableAuth(ctx, auth); err != nil {
			return err
		}
	} else {
		if err := r.disableAuth(ctx, auth); err != nil {
			return err
		}
	}

	err = testMemberList(ctx, cli)
	if err != nil {
		return err
	}

	return err
}

// This is auxiliary self-test function, that shows that connection to etcd cluster works.
// As soon as operator has functionality to operate etcd-cluster, this function can be removed.
func testMemberList(ctx context.Context, cli *clientv3.Client) error {

	etcdCluster := clientv3.NewCluster(cli)

	_, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	memberList, err := etcdCluster.MemberList(ctx)

	if err != nil {
		log.Error(ctx, err, "failed to get member list", "endpoints", cli.Endpoints())
		return err
	}
	log.Debug(ctx, "member list got", "member list", memberList)

	return err
}

func (r *EtcdClusterReconciler) GetEtcdClient(ctx context.Context, instance *etcdaenixiov1alpha1.EtcdCluster) (*clientv3.Client, error) {

	endpoints := getEndpointsSlice(instance)
	log.Debug(ctx, "endpoints built", "endpoints", endpoints)

	tlsConfig, err := r.getTLSConfig(ctx, instance)
	if err != nil {
		log.Error(ctx, err, "failed to build tls config")
		return nil, err
	}
	log.Debug(ctx, "tls config built", "tls config", tlsConfig)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		TLS:         tlsConfig,
	},
	)
	if err != nil {
		log.Error(ctx, err, "failed to create etcd client", "endpoints", endpoints)
		return nil, err
	}
	log.Debug(ctx, "etcd client created", "endpoints", endpoints)

	return cli, nil

}

func (r *EtcdClusterReconciler) getTLSConfig(ctx context.Context, instance *etcdaenixiov1alpha1.EtcdCluster) (*tls.Config, error) {

	var err error

	caCertPool := &x509.CertPool{}

	if instance.IsServerTrustedCADefined() {

		serverCASecret := &corev1.Secret{}

		if err = r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Spec.Security.TLS.ServerTrustedCASecret}, serverCASecret); err != nil {
			log.Error(ctx, err, "failed to get server trusted CA secret")
			return nil, err
		}
		log.Debug(ctx, "secret read", "server trusted CA secret", serverCASecret)

		caCertPool = x509.NewCertPool()

		if !caCertPool.AppendCertsFromPEM(serverCASecret.Data["tls.crt"]) {
			log.Error(ctx, err, "failed to parse CA certificate")
			return nil, err
		}

	}

	cert := tls.Certificate{}

	if instance.IsClientSecurityEnabled() {

		rootSecret := &corev1.Secret{}
		if err = r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Spec.Security.TLS.ClientSecret}, rootSecret); err != nil {
			log.Error(ctx, err, "failed to get root client secret")
			return nil, err
		}
		log.Debug(ctx, "secret read", "root client secret", rootSecret)

		cert, err = tls.X509KeyPair(rootSecret.Data["tls.crt"], rootSecret.Data["tls.key"])
		if err != nil {
			log.Error(ctx, err, "failed to parse key pair", "cert", cert)
			return nil, err
		}
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: !instance.IsServerTrustedCADefined(),
		RootCAs:            caCertPool,
		Certificates: []tls.Certificate{
			cert,
		},
	}

	return tlsConfig, err
}

func getEndpointsSlice(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {

	endpoints := []string{}
	for podNumber := 0; podNumber < int(*cluster.Spec.Replicas); podNumber++ {
		endpoints = append(
			endpoints,
			strings.Join(
				[]string{
					factory.GetServerProtocol(cluster) + cluster.Name + "-" + strconv.Itoa(podNumber),
					factory.GetHeadlessServiceName(cluster),
					cluster.Namespace,
					"svc:2379"},
				"."))
	}
	return endpoints
}

func (r *EtcdClusterReconciler) createRoleIfNotExists(ctx context.Context, authClient clientv3.Auth, roleName string) error {

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := authClient.RoleGet(ctx, roleName)
	if err != nil {
		if err.Error() != "etcdserver: role name not found" {
			log.Error(ctx, err, "failed to get role", "role name", "root")
			return err
		}
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err = authClient.RoleAdd(ctx, roleName)
		if err != nil {
			log.Error(ctx, err, "failed to add role", "role name", "root")
			return err
		}
		log.Debug(ctx, "role added", "role name", "root")
		return nil
	}
	log.Debug(ctx, "role exists, nothing to do", "role name", "root")

	return nil
}

func (r *EtcdClusterReconciler) createUserIfNotExists(ctx context.Context, authClient clientv3.Auth, userName string) (*clientv3.AuthUserGetResponse, error) {

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	userResponse, err := authClient.UserGet(ctx, userName)
	if err != nil {
		if err.Error() != "etcdserver: user name not found" {
			log.Error(ctx, err, "failed to get user", "user name", "root")
			return nil, err
		}

		_, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err = authClient.UserAddWithOptions(ctx, "root", "", &clientv3.UserAddOptions{
			NoPassword: true,
		})
		if err != nil {
			log.Error(ctx, err, "failed to add user", "user name", "root")
			return nil, err
		}
		log.Debug(ctx, "user added", "user name", "root")
		return nil, nil
	}
	log.Debug(ctx, "user exists, nothing to do", "user name", "root")

	return userResponse, err
}

func (r *EtcdClusterReconciler) grantRoleToUser(ctx context.Context, authClient clientv3.Auth, userName, roleName string, userResponse *clientv3.AuthUserGetResponse) error {

	var err error

	if userResponse == nil || !slices.Contains(userResponse.Roles, roleName) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := authClient.UserGrantRole(ctx, userName, roleName)

		if err != nil {
			log.Error(ctx, err, "failed to grant user to role", "user:role name", "root:root")
			return err
		}
		log.Debug(ctx, "user:role granted", "user:role name", "root:root")
	} else {
		log.Debug(ctx, "user:role already granted, nothing to do", "user:role name", "root:root")
	}

	return err
}

func (r *EtcdClusterReconciler) enableAuth(ctx context.Context, authClient clientv3.Auth) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := authClient.AuthEnable(ctx)

	if err != nil {
		log.Error(ctx, err, "failed to enable auth")
		return err
	}
	log.Debug(ctx, "auth enabled")

	return err
}

func (r *EtcdClusterReconciler) disableAuth(ctx context.Context, authClient clientv3.Auth) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := authClient.AuthDisable(ctx)
	if err != nil {
		log.Error(ctx, err, "failed to disable auth")
		return err
	}
	log.Debug(ctx, "auth disabled")

	return nil
}

// ensureUnconditionalObjects creates the two services and the PDB
// which can be created at the start of the reconciliation loop
// without any risk of disrupting the etcd cluster
func (r *EtcdClusterReconciler) ensureUnconditionalObjects(ctx context.Context, state *observables) error {
	const concurrentOperations = 3
	c := make(chan error)
	defer close(c)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(concurrentOperations)
	wrapWithMsg := func(err error, msg string) error {
		if err != nil {
			return fmt.Errorf(msg+": %w", err)
		}
		return nil
	}
	go func(chan<- error) {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case c <- wrapWithMsg(factory.CreateOrUpdateClientService(ctx, state.instance, r.Client),
			"couldn't ensure client service"):
		}
	}(c)
	go func(chan<- error) {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case c <- wrapWithMsg(factory.CreateOrUpdateHeadlessService(ctx, state.instance, r.Client),
			"couldn't ensure headless service"):
		}
	}(c)
	go func(chan<- error) {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case c <- wrapWithMsg(factory.CreateOrUpdatePdb(ctx, state.instance, r.Client),
			"couldn't ensure pod disruption budget"):
		}
	}(c)

	for i := 0; i < concurrentOperations; i++ {
		if err := <-c; err != nil {
			cancel()

			// let all goroutines select the ctx.Done() case to avoid races on closed channels
			wg.Wait()
			return err
		}
	}
	return nil
}

// TODO!
// nolint:unused
func (r *EtcdClusterReconciler) patchOrCreateObject(ctx context.Context, obj client.Object) error {
	err := r.Patch(ctx, obj, client.Apply, &client.PatchOptions{FieldManager: "etcd-operator"}, client.ForceOwnership)
	if err == nil {
		return nil
	}
	if client.IgnoreNotFound(err) == nil {
		err = r.Create(ctx, obj)
	}
	return err
}

// TODO!
// nolint:unparam,unused
func (r *EtcdClusterReconciler) createClusterFromScratch(ctx context.Context, state *observables) (ctrl.Result, error) {
	cm := factory.TemplateClusterStateConfigMap(state.instance, "new", state.desiredReplicas())
	err := ctrl.SetControllerReference(state.instance, cm, r.Scheme)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.patchOrCreateObject(ctx, cm)
	if err != nil {
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(
		&state.instance.Status.Conditions,
		metav1.Condition{
			Type:    etcdaenixiov1alpha1.EtcdConditionInitialized,
			Status:  metav1.ConditionFalse,
			Reason:  string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted),
			Message: string(etcdaenixiov1alpha1.EtcdInitCondNegMessage),
		},
	)
	meta.SetStatusCondition(
		&state.instance.Status.Conditions,
		metav1.Condition{
			Type:    etcdaenixiov1alpha1.EtcdConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  string(etcdaenixiov1alpha1.EtcdCondTypeWaitingForFirstQuorum),
			Message: string(etcdaenixiov1alpha1.EtcdReadyCondNegWaitingForQuorum),
		},
	)

	// ensure managed resources
	if err = r.ensureConditionalClusterObjects(ctx, state); err != nil {
		return r.updateStatusOnErr(ctx, state, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err))
	}
	panic("not yet implemented")
}

// TODO!
// nolint:unused
func (r *EtcdClusterReconciler) scaleUpFromZero(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

// TODO!
// nolint:unused
func (r *EtcdClusterReconciler) createOrUpdateClusterStateConfigMap(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

// TODO!
// nolint:unused
func (r *EtcdClusterReconciler) createOrUpdateStatefulSet(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

// TODO!
// nolint:unused
func (r *EtcdClusterReconciler) promoteLearners(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}
