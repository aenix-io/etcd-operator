package v1alpha1_test

import (
	"github.com/aenix-io/etcd-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Aux Functions", func() {

	Context("When running IsClientSecurityEnabled function", func() {
		It("should return true if ClientSecret is set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{
							ClientSecret: "some-client-secret",
						},
					},
				},
			}
			Expect(v1alpha1.IsClientSecurityEnabled(cluster)).To(BeTrue())
		})

		It("should return false if ClientSecret is not set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{},
					},
				},
			}
			Expect(v1alpha1.IsClientSecurityEnabled(cluster)).To(BeFalse())
		})

		It("should return false if Security is nil", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{},
			}
			Expect(v1alpha1.IsClientSecurityEnabled(cluster)).To(BeFalse())
		})
	})

	Context("When running IsServerSecurityEnabled function", func() {
		It("should return true if ServerSecret is set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{
							ServerSecret: "some-server-secret",
						},
					},
				},
			}
			Expect(v1alpha1.IsServerSecurityEnabled(cluster)).To(BeTrue())
		})

		It("should return false if ServerSecret is not set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{},
					},
				},
			}
			Expect(v1alpha1.IsServerSecurityEnabled(cluster)).To(BeFalse())
		})

		It("should return false if Security is nil", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{},
			}
			Expect(v1alpha1.IsServerSecurityEnabled(cluster)).To(BeFalse())
		})
	})

	Context("When running IsServerCADefined function", func() {
		It("should return true if ServerTrustedCASecret is set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{
							ServerTrustedCASecret: "some-ca-secret",
						},
					},
				},
			}
			Expect(v1alpha1.IsServerCADefined(cluster)).To(BeTrue())
		})

		It("should return false if ServerTrustedCASecret is not set", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{
					Security: &v1alpha1.SecuritySpec{
						TLS: v1alpha1.TLSSpec{},
					},
				},
			}
			Expect(v1alpha1.IsServerCADefined(cluster)).To(BeFalse())
		})

		It("should return false if Security is nil", func() {
			cluster := &v1alpha1.EtcdCluster{
				Spec: v1alpha1.EtcdClusterSpec{},
			}
			Expect(v1alpha1.IsServerCADefined(cluster)).To(BeFalse())
		})
	})
})
