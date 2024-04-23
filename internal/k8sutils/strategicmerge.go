package k8sutils

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// StrategicMerge merges two objects using strategic merge patch.
// It accepts two objects, base and patch, and returns a merged object.
// Both base and patch objects must be of the same type and must not be a pointer.
func StrategicMerge[K any](base, patch K) (merged K, err error) {
	baseBytes, err := json.Marshal(base)
	if err != nil {
		return merged, fmt.Errorf("cannot marshal base object to JSON: %w", err)
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return merged, fmt.Errorf("cannot marshal patch object to JSON: %w", err)
	}

	mergedBytes, err := strategicpatch.StrategicMergePatch(baseBytes, patchBytes, &merged)
	if err != nil {
		return merged, fmt.Errorf("cannot patch base object with given spec: %w", err)
	}

	err = json.Unmarshal(mergedBytes, &merged)
	if err != nil {
		return merged, fmt.Errorf("cannot unmarshal merged object from JSON: %w", err)
	}

	return merged, nil
}
