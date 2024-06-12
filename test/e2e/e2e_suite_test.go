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
	"context"
	"fmt"
	"testing"

	"github.com/aenix-io/etcd-operator/internal/log"
	"github.com/go-logr/logr"
	logctrl "sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var ctx context.Context

var _ = BeforeSuite(func() {
	ctx = log.Setup(context.TODO(), log.Parameters{
		LogLevel:        "error",
		StacktraceLevel: "error",
		Development:     true,
	})

	// This line prevents controller-runtime from complaining about log.SetLogger never being called
	logctrl.SetLogger(logr.FromContextOrDiscard(ctx))
})

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, err := fmt.Fprintf(GinkgoWriter, "Starting etcd-operator suite\n")
	if err != nil {
		return
	}
	RunSpecs(t, "e2e suite")
}
