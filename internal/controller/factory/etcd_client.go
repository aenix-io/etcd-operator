package factory

import (
	"context"
	"fmt"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
	clientv3 "go.etcd.io/etcd/client/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewEtcdClientSet(ctx context.Context, cluster *v1alpha1.EtcdCluster, cli client.Client) (*clientv3.Client, []*clientv3.Client, error) {
	cfg, err := configFromCluster(ctx, cluster, cli)
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
	singleClients := make([]*clientv3.Client, len(eps))
	for i, ep := range eps {
		cfg.Endpoints = []string{ep}
		singleClients[i], err = clientv3.New(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("error building etcd single-endpoint client for endpoint %s: %w", ep, err)
		}
	}
	return clusterClient, singleClients, nil
}

func configFromCluster(ctx context.Context, cluster *v1alpha1.EtcdCluster, cli client.Client) (clientv3.Config, error) {
	ep := v1.Endpoints{}
	err := cli.Get(ctx, types.NamespacedName{Name: GetHeadlessServiceName(cluster), Namespace: cluster.Namespace}, &ep)
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
		urls = append(urls, fmt.Sprintf("%s:%s", name, "2379"))
	}

	return clientv3.Config{Endpoints: urls}, nil
}
