package k8sutils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("StrategicMerge handler", func() {
	When("Merging two podspecs", func() {
		It("Should merge two empty podspecs into an empty podspec", func() {
			p1 := corev1.PodSpec{}
			p2 := corev1.PodSpec{}
			p, err := StrategicMerge(p1, p2)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).To(Equal(p1))
		})
	})
})
