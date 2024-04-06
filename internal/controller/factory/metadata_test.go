package factory

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
)

var _ = Describe("Metadata merge", func() {
	Context("When ensuring a metadata merge", func() {
		metadata := metav1.ObjectMeta{
			Name: "test",
			Labels: map[string]string{
				"label":  "value",
				"label2": "value2",
			},
			Annotations: map[string]string{
				"annotation":  "value",
				"annotation2": "value2",
			},
		}

		It("metadata should merge with empty override", func() {
			merged := mergeObjectMeta(metadata, v1alpha1.EmbeddedObjectMetadata{})
			Expect(merged.Name).To(Equal(metadata.Name))
			Expect(merged.Labels).To(Equal(metadata.Labels))
			Expect(merged.Annotations).To(Equal(metadata.Annotations))
		})

		It("metadata should merge with override", func() {
			override := v1alpha1.EmbeddedObjectMetadata{
				Labels: map[string]string{
					"label":  "override",
					"label3": "value3",
				},
				Annotations: map[string]string{
					"annotation":  "override",
					"annotation3": "value3",
				},
			}
			merged := mergeObjectMeta(metadata, override)
			Expect(merged.Name).To(Equal(metadata.Name))
			Expect(merged.Labels).To(Equal(override.Labels))
			Expect(merged.Annotations).To(Equal(override.Annotations))
		})

		It("metadata should merge with partial override", func() {
			override := v1alpha1.EmbeddedObjectMetadata{
				Labels: map[string]string{
					"label": "override",
				},
				Annotations: map[string]string{
					"annotation": "override",
				},
			}
			merged := mergeObjectMeta(metadata, override)
			Expect(merged.Name).To(Equal(metadata.Name))
			Expect(merged.Labels).To(Equal(override.Labels))
			Expect(merged.Annotations).To(Equal(override.Annotations))
		})
	})
})
