package factory

import (
	"fmt"
)

// ArgsFromMapToSlice create the slice of args
// Along with that, if a flag doesn't have a value, it's presented barely without a value assignment.
func argsFromMapToSlice(args map[string]string) (slice []string) {
	for flag, value := range args {
		if len(value) == 0 {
			slice = append(slice, flag)

			continue
		}

		slice = append(slice, fmt.Sprintf("%s=%s", flag, value))
	}

	return slice
}
