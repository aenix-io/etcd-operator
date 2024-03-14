/*
Copyright 2024.

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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("EtcdCluster Webhook", func() {

	Context("When creating EtcdCluster under Defaulting Webhook", func() {
		It("Should fill in the default value if a required field is empty", func() {
			etcdCluster := &EtcdCluster{}
			etcdCluster.Default()
			gomega.Expect(etcdCluster.Spec.Replicas).To(gomega.Equal(uint(3)))
			gomega.Expect(etcdCluster.Spec.Storage.Size).To(gomega.Equal(resource.MustParse("4Gi")))
		})

		It("Should not override fields with default values if not empty", func() {
			etcdCluster := &EtcdCluster{
				Spec: EtcdClusterSpec{
					Replicas: 5,
					Storage: Storage{
						StorageClass: "local-path",
						Size:         resource.MustParse("10Gi"),
					},
				},
			}
			etcdCluster.Default()
			gomega.Expect(etcdCluster.Spec.Replicas).To(gomega.Equal(uint(5)))
			gomega.Expect(etcdCluster.Spec.Storage.Size).To(gomega.Equal(resource.MustParse("10Gi")))
		})
	})

	// Not yet applicable as currently all fields are optional.

	//Context("When creating EtcdCluster under Validating Webhook", func() {
	//	It("Should deny if a required field is empty", func() {
	//
	//		// TODO(user): Add your logic here
	//
	//	})
	//
	//	It("Should admit if all required fields are provided", func() {
	//
	//		// TODO(user): Add your logic here
	//
	//	})
	//})

})
