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

	"github.com/aenix-io/etcd-operator/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("controller", Ordered, func() {

	BeforeAll(func() {
		By("prepare kind environment")
		cmd := exec.Command("make", "kind-prepare")
		_, _ = utils.Run(cmd)
		By("upload latest etcd-operator docker image to kind cluster")
		cmd = exec.Command("make", "kind-load")
		_, _ = utils.Run(cmd)
	})

	AfterAll(func() {
		By("Delete kind environment")
		cmd := exec.Command("make", "kind-delete")
		_, _ = utils.Run(cmd)
	})

	Context("Operator", func() {
		It("should run successfully", func() {
			var err error
			By("deploy etcd-operator")
			cmd := exec.Command("make", "deploy")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})

		It("should deploy simple etcd cluster", func() {
			var err error
			const namespace = "test-simple-etcd-cluster"

			By("apply simple etcd cluster manifest")
			cmd := exec.Command("kubectl", "create", "namespace", namespace)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			//cmd = exec.Command("kubectl", "apply",
			//	"--filename", "/home/hiddenmarten/Git/github/aenix-io/etcd-operator/examples/manifests/etcdcluster-simple.yaml",
			//	"--namespace", namespace,
			//)
			//_, err = utils.Run(cmd)
			//ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})

	})
})
