package k8sutils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestK8SUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8SUtils Suite")
}
