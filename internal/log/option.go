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
	"io"
	"log/slog"
)

type Option interface {
	apply(*Handler)
}

type loggingLevel struct {
	level slog.Leveler
}

func (o loggingLevel) apply(h *Handler) {
	h.level = o.level
}

func WithLevel(l slog.Leveler) Option {
	return loggingLevel{level: l}
}

type stacktrace struct {
	enabled bool
	level   slog.Leveler
}

func (o stacktrace) apply(h *Handler) {
	h.stacktraceEnabled = o.enabled
	h.stacktraceLevel = o.level
}

func WithStacktrace(enabled bool, level slog.Leveler) Option {
	return stacktrace{enabled: enabled, level: level}
}

type writer struct {
	w io.Writer
}

func (o writer) apply(h *Handler) {
	h.writer = o.w
}

func WithWriter(w io.Writer) Option {
	return writer{w: w}
}

func applyForHandler(h *Handler, opts ...Option) {
	for _, opt := range opts {
		opt.apply(h)
	}
}
