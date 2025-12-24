package factory

import (
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func NewEtcdClientSet(cfg clientv3.Config) (*clientv3.Client, []*clientv3.Client, error) {
	// cfg, err := configFromCluster(ctx, cluster, cli)
	// if err != nil {
	// 	return nil, nil, err
	// }
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
			// nolint:errcheck
			clusterClient.Close()
			return nil, nil, fmt.Errorf("error building etcd single-endpoint client for endpoint %s: %w", ep, err)
		}
	}
	return clusterClient, singleClients, nil
}

