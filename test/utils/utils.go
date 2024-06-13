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

package utils

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	clientv3 "go.etcd.io/etcd/client/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	"github.com/aenix-io/etcd-operator/internal/controller"
	"github.com/aenix-io/etcd-operator/internal/log"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) ([]byte, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, err := fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
		if err != nil {
			return nil, err
		}
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, err := fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	if err != nil {
		return nil, err
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort(ctx context.Context) (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer func(l *net.TCPListener) {
				err := l.Close()
				if err != nil {
					log.Error(ctx, err, "failed to get free port")
				}
			}(l)
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func GetK8sClient(ctx context.Context) (controller.EtcdClusterReconciler, error) {

	cfg, err := config.GetConfig()
	if err != nil {
		return controller.EtcdClusterReconciler{}, err
	}

	c, err := client.New(cfg, client.Options{Scheme: runtimeScheme})
	if err != nil {
		return controller.EtcdClusterReconciler{}, err
	}

	r := controller.EtcdClusterReconciler{
		Client: c,
	}

	return r, err
}

var (
	runtimeScheme = runtime.NewScheme()
)

func init() {
	_ = etcdaenixiov1alpha1.AddToScheme(runtimeScheme)
	_ = corev1.AddToScheme(runtimeScheme)
}

// IsEtcdClusterHealthy checks etcd cluster health.
func IsEtcdClusterHealthy(ctx context.Context, client *clientv3.Client) (bool, error) {
	// Should be changed when etcd is healthy
	health := false
	var err error

	// Prepare the maintenance client
	maint := clientv3.NewMaintenance(client)

	// Context for the call
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Perform the status call to check health
	for _, endpoint := range client.Endpoints() {
		resp, err := maint.Status(ctx, endpoint)
		if err != nil {
			return false, err
		} else {
			if resp.Errors == nil {
				fmt.Printf("Endpoint is healthy: %s\n", resp.Version)
				health = true
			}
		}
	}
	return health, err
}

func GetEtcdClient(ctx context.Context, namespacedName types.NamespacedName) (*clientv3.Client, error) {

	r, err := GetK8sClient(ctx)
	if err != nil {
		return nil, err
	}

	cluster := &etcdaenixiov1alpha1.EtcdCluster{}
	err = r.Get(ctx, namespacedName, cluster)
	if err != nil {
		return nil, err
	}

	client, err := r.GetEtcdClient(ctx, cluster)
	if err != nil {
		return nil, err
	}

	port, _ := getFreePort(ctx)
	go func() {
		cmd := exec.Command("kubectl", "port-forward",
			"service/test", strconv.Itoa(port)+":2379",
			"--namespace", cluster.Namespace,
		)
		_, err = Run(cmd)
	}()

	localEndpoints := []string{}

	for _, endpoint := range client.Endpoints() {
		re := regexp.MustCompile(`:\/\/.*`)
		localEndpoints = append(localEndpoints, re.ReplaceAllString(endpoint, "://localhost:"+strconv.Itoa(port)))
	}

	client.SetEndpoints(localEndpoints...)

	return client, err
}
