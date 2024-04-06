package factory

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aenix-io/etcd-operator/api/v1alpha1"
)

// mergeObjectMeta merges the metadata of an existing object with the metadata of a new object.
// It copies the labels and annotations from the new object to the existing object.
func mergeObjectMeta(old metav1.ObjectMeta, new v1alpha1.EmbeddedObjectMetadata) metav1.ObjectMeta {
	metadata := old.DeepCopy()

	if new.Labels != nil && len(new.Labels) > 0 {
		metadata.Labels = new.Labels
	}
	if new.Annotations != nil && len(new.Annotations) > 0 {
		metadata.Annotations = new.Annotations
	}

	return *metadata
}
