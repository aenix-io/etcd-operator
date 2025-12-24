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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var _ = Describe("NewEtcdClientSet", func() {

	Context("when no endpoints are provided", func() {
		It("returns nils without error", func() {
			cfg := clientv3.Config{
				Endpoints: nil,
			}

			cluster, singles, err := NewEtcdClientSet(cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster).To(BeNil())
			Expect(singles).To(BeNil())
		})
	})

	Context("when configuration is valid", func() {
		It("creates cluster and single clients", func() {
			cfg := clientv3.Config{
				Endpoints: []string{
					"localhost:2379",
					"localhost:2380",
				},
				DialTimeout: 1 * time.Second,
			}

			cluster, singles, err := NewEtcdClientSet(cfg)

			Expect(err).ToNot(HaveOccurred())
			Expect(cluster).ToNot(BeNil())
			Expect(singles).To(HaveLen(2))

			for _, c := range singles {
				Expect(c).ToNot(BeNil())
			}

			_ = cluster.Close()
			for _, c := range singles {
				_ = c.Close()
			}
		})
	})
})
