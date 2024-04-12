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

package e2e

import (
	"os/exec"
	"strconv"
	"sync"

	"github.com/aenix-io/etcd-operator/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("etcd-operator", Ordered, func() {

	BeforeAll(func() {
		var err error
		By("prepare kind environment")
		cmd := exec.Command("make", "kind-prepare")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		By("upload latest etcd-operator docker image to kind cluster")
		cmd = exec.Command("make", "kind-load")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		By("deploy etcd-operator")
		cmd = exec.Command("make", "deploy")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("Delete kind environment")
		cmd := exec.Command("make", "kind-delete")
		_, _ = utils.Run(cmd)
	})

	Context("Simple", func() {
		It("should deploy etcd cluster", func() {
			var err error
			const namespace = "test-simple-etcd-cluster"
			var wg sync.WaitGroup
			wg.Add(1)

			By("create namespace")
			cmd := exec.Command("kubectl", "create", "namespace", namespace)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("apply simple etcd cluster manifest")
			dir, _ := utils.GetProjectDir()
			cmd = exec.Command("kubectl", "apply",
				"--filename", dir+"/examples/manifests/etcdcluster-simple.yaml",
				"--namespace", namespace,
			)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("wait for statefulset is ready")
			cmd = exec.Command("kubectl", "wait",
				"statefulset/test",
				"--for", "jsonpath={.status.availableReplicas}=3",
				"--namespace", namespace,
				"--timeout", "5m",
			)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("port-forward service to localhost")
			port, _ := utils.GetFreePort()
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				cmd = exec.Command("kubectl", "port-forward",
					"service/test-client", strconv.Itoa(port)+":2379",
					"--namespace", namespace,
				)
				_, err = utils.Run(cmd)
			}()

			By("check etcd cluster is healthy")
			endpoints := []string{"localhost:" + strconv.Itoa(port)}
			for i := 0; i < 3; i++ {
				Expect(utils.IsEtcdClusterHealthy(endpoints)).To(BeTrue())
			}
		})
	})
})
