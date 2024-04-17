package k8sutils

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

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
		return merged, fmt.Errorf("cannot patch base pod spec with podTemplate.spec: %w", err)
	}

	err = json.Unmarshal(mergedBytes, &merged)
	if err != nil {
		return merged, fmt.Errorf("cannot unmarshal merged object from JSON: %w", err)
	}

	return merged, nil
}
