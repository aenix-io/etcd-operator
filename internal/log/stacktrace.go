/*
Copyright 2024 The etcd-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"fmt"
	"runtime"
)

type stack struct {
	pcs     []uintptr
	frames  *runtime.Frames
	storage []uintptr
}

func (s *stack) next() (runtime.Frame, bool) {
	return s.frames.Next()
}

type depth int

const (
	first depth = iota
	full
)

func capture(skip int, depth depth) *stack {
	st := &stack{
		storage: make([]uintptr, 64),
	}

	switch depth {
	case first:
		st.pcs = st.storage[:1]
	case full:
		st.pcs = st.storage
	}

	numFrames := runtime.Callers(
		skip+2,
		st.pcs,
	)

	if depth == full {
		pcs := st.pcs
		for numFrames == len(pcs) {
			pcs = make([]uintptr, len(pcs)*2)
			numFrames = runtime.Callers(skip+2, pcs)
		}

		st.storage = pcs
		st.pcs = pcs[:numFrames]
	} else {
		st.pcs = st.pcs[:numFrames]
	}

	st.frames = runtime.CallersFrames(st.pcs)
	return st
}

func getStacktrace(skip int) []string {
	st := capture(skip+1, full)
	result := make([]string, 0)
	for frame, more := st.next(); more; frame, more = st.next() {
		result = append(result, fmt.Sprintf("%s:%d:%s", frame.File, frame.Line, frame.Function))
	}
	return result
}
