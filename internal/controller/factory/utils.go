package factory

import (
	"strings"
)

// argsFromSliceToMap transforms a slice of string into a map
func argsFromSliceToMap(args []string) (m map[string]string) {
	m = make(map[string]string)

	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)

		flag, value := parts[0], ""

		if len(parts) > 1 {
			value = parts[1]
		}

		m[flag] = value
	}

	return m
}
