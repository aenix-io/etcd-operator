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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clientv3 "go.etcd.io/etcd/client/v3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aenix-io/etcd-operator/test/utils"
)

var _ = Describe("etcd-operator", Ordered, func() {

	BeforeAll(func() {
		var err error
		By("prepare kind environment", func() {
			cmd := exec.Command("make", "kind-prepare")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})

		By("upload latest etcd-operator docker image to kind cluster", func() {
			cmd := exec.Command("make", "kind-load")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})

		By("deploy etcd-operator", func() {
			cmd := exec.Command("make", "deploy")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})

		By("wait while etcd-operator is ready", func() {
			cmd := exec.Command("kubectl", "wait", "--namespace",
				"etcd-operator-system", "deployment/etcd-operator-controller-manager",
				"--for", "jsonpath={.status.availableReplicas}=1", "--timeout=5m")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})
	})

	if os.Getenv("DO_CLEANUP_AFTER_E2E") == "true" {
		AfterAll(func() {
			By("Delete kind environment", func() {
				cmd := exec.Command("make", "kind-delete")
				_, _ = utils.Run(cmd)
			})
		})
	}

	Context("With PVC and resize", func() {
		const namespace = "test-pvc-and-resize-etcd-cluster"
		const storageClass = `
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: standard-with-expansion
provisioner: rancher.io/local-path
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
`

		It("should resize PVCs of the etcd cluster", func() {
			var err error
			By("create namespace", func() {
				cmd := exec.Command("kubectl", "create", "namespace", namespace)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			})

			By("create StorageClass", func() {
				cmd := exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(storageClass)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			})

			By("deploying etcd cluster with initial PVC size", func() {
				dir, _ := utils.GetProjectDir()
				cmd := exec.Command("kubectl", "apply",
					"--filename", dir+"/examples/manifests/etcdcluster-persistent.yaml",
					"--namespace", namespace,
				)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			})

			By("waiting for statefulset to be ready", func() {
				Eventually(func() error {
					cmd := exec.Command("kubectl", "wait",
						"statefulset/test",
						"--for", "jsonpath={.status.readyReplicas}=3",
						"--namespace", namespace,
						"--timeout", "5m",
					)
					_, err = utils.Run(cmd)
					return err
				}, 5*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("updating the storage request", func() {
				// Patch the EtcdCluster to increase storage size
				patch := `{"spec": {"storage": {"volumeClaimTemplate": {"spec": {"resources": {"requests": {"storage": "8Gi"}}}}}}}`
				cmd := exec.Command("kubectl", "patch", "etcdcluster", "test", "--namespace", namespace, "--type", "merge", "--patch", patch) //nolint:lll
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			})

			By("checking that PVC sizes have been updated", func() {
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pvc", "-n", namespace, "-o", "jsonpath={.items[*].spec.resources.requests.storage}") //nolint:lll
					output, err := utils.Run(cmd)
					if err != nil {
						return false
					}
					// Split the output into individual sizes and check each one
					sizes := strings.Fields(string(output))
					for _, size := range sizes {
						if size != "8Gi" {
							return false
						}
					}
					return true
				}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "PVCs should be resized to 8Gi")
			})
		})
	})

	Context("With emptyDir", func() {
		It("should deploy etcd cluster", func() {
			var err error
			const namespace = "test-emptydir-etcd-cluster"
			var wg sync.WaitGroup
			wg.Add(1)

			By("create namespace", func() {
				cmd := exec.Command("sh", "-c",
					fmt.Sprintf("kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f -", namespace)) // nolint:lll
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
			})

			By("apply etcd cluster with emptydir manifest", func() {
				dir, _ := utils.GetProjectDir()
				cmd := exec.Command("kubectl", "apply",
					"--filename", dir+"/examples/manifests/etcdcluster-emptydir.yaml",
					"--namespace", namespace,
				)
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
			})

			Eventually(func() error {
				cmd := exec.Command("kubectl", "wait",
					"statefulset/test",
					"--for", "jsonpath={.status.readyReplicas}=3",
					"--namespace", namespace,
					"--timeout", "5m",
				)
				_, err = utils.Run(cmd)
				return err
			}, time.Second*20, time.Second*2).Should(Succeed(), "wait for statefulset is ready")

			client, err := utils.GetEtcdClient(ctx, client.ObjectKey{Namespace: namespace, Name: "test"})
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err := client.Close()
				Expect(err).NotTo(HaveOccurred())
			}()

			By("check etcd cluster is healthy", func() {
				Expect(utils.IsEtcdClusterHealthy(ctx, client)).To(BeTrue())
			})
		})
	})

	Context("TLS and enabled auth", func() {
		It("should deploy etcd cluster with auth", func() {
			var err error
			const namespace = "test-tls-auth-etcd-cluster"
			var wg sync.WaitGroup
			wg.Add(1)

			By("create namespace", func() {
				cmd := exec.Command("sh", "-c", fmt.Sprintf("kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f -", namespace)) //nolint:lll
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
			})

			By("apply tls with enabled auth etcd cluster manifest", func() {
				dir, _ := utils.GetProjectDir()
				cmd := exec.Command("kubectl", "apply",
					"--filename", dir+"/examples/manifests/etcdcluster-with-external-certificates.yaml",
					"--namespace", namespace,
				)
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
			})

			Eventually(func() error {
				cmd := exec.Command("kubectl", "wait",
					"certificate/client-certificate",
					"--for", "condition=Ready",
					"--namespace", namespace,
					"--timeout", "5m",
				)
				_, err = utils.Run(cmd)
				return err
			}, time.Second*20, time.Second*2).Should(Succeed(), "wait for client cert ready")

			Eventually(func() error {
				cmd := exec.Command("kubectl", "wait",
					"statefulset/test",
					"--for", "jsonpath={.status.availableReplicas}=3",
					"--namespace", namespace,
					"--timeout", "5m",
				)
				_, err = utils.Run(cmd)
				return err
			}, time.Second*20, time.Second*2).Should(Succeed(), "wait for statefulset is ready")

			client, err := utils.GetEtcdClient(ctx, client.ObjectKey{Namespace: namespace, Name: "test"})
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err := client.Close()
				Expect(err).NotTo(HaveOccurred())
			}()

			By("check etcd cluster is healthy", func() {
				Expect(utils.IsEtcdClusterHealthy(ctx, client)).To(BeTrue())
			})

			auth := clientv3.NewAuth(client)

			By("check root role is created", func() {
				Eventually(func() error {
					_, err = auth.RoleGet(ctx, "root")
					return err
				}, time.Second*20, time.Second*2).Should(Succeed())
			})

			By("check root user is created and has root role", func() {
				userResponce, err := auth.UserGet(ctx, "root")
				Expect(err).NotTo(HaveOccurred())
				Expect(userResponce.Roles).To(ContainElement("root"))
			})

			By("check auth is enabled", func() {
				authStatus, err := auth.AuthStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(authStatus.Enabled).To(BeTrue())
			})
		})
	})
})
