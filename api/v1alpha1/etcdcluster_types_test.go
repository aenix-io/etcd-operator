package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Context("CalculateQuorumSize", func() {
	It("should return correct result for odd number of replicas", func() {
		etcdCluster := EtcdCluster{
			Spec: EtcdClusterSpec{Replicas: ptr.To(int32(3))},
		}
		Expect(etcdCluster.CalculateQuorumSize()).To(Equal(2))
	})
	It("should return correct result for even number of replicas", func() {
		etcdCluster := EtcdCluster{
			Spec: EtcdClusterSpec{Replicas: ptr.To(int32(4))},
		}
		Expect(etcdCluster.CalculateQuorumSize()).To(Equal(3))
	})
})
