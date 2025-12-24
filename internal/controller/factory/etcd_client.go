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
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func NewEtcdClientSet(cfg clientv3.Config) (*clientv3.Client, []*clientv3.Client, error) {
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

