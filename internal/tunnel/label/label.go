package label

import (
	"fmt"
	"sort"
	"strings"
)

type Labels map[string]string

func ToString(labels Labels) string {
	labelPairs := make([]string, 0, len(labels))
	for k, v := range labels {
		labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(labelPairs)
	return strings.Join(labelPairs, ", ")
}

// expects a list of strings in the format "foo=bar"
func ParseAndMerge(labelKvs []string) (Labels, error) {
	labels := make(map[string]string, len(labelKvs))
	for _, kv := range labelKvs {
		k, v, valid := strings.Cut(kv, "=")
		if !valid {
			return nil, fmt.Errorf("unexpected formatting for label %s", kv)
		}
		if existingVal, ok := labels[k]; ok {
			// don't overwrite existing key
			return nil, fmt.Errorf("label key %s already present with value %s", k, existingVal)
		}
		labels[k] = v
	}

	return labels, nil
}
