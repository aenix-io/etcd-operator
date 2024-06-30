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
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

type Handler struct {
	slog.Handler
	writer            io.Writer
	level             slog.Leveler
	stacktraceEnabled bool
	stacktraceLevel   slog.Leveler
}

func NewHandler(opts ...Option) *Handler {
	handler := &Handler{
		writer:          os.Stdout,
		level:           slog.LevelInfo,
		stacktraceLevel: slog.LevelError,
	}
	applyForHandler(handler, opts...)
	handler.Handler = slog.NewJSONHandler(handler.writer, &slog.HandlerOptions{
		Level:       handler.level,
		ReplaceAttr: replaceCommonKeyValues,
	})

	return handler
}

func (h *Handler) stacktrace(level slog.Level) bool {
	return h.stacktraceEnabled && level >= h.stacktraceLevel.Level()
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if h.stacktrace(r.Level) {
		trace := getStacktrace(4)
		r.AddAttrs(slog.Attr{Key: "stacktrace", Value: slog.AnyValue(trace)})
	}
	return h.Handler.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handler := h.clone()
	handler.Handler = handler.Handler.WithAttrs(attrs)
	return handler
}

func (h *Handler) WithGroup(name string) slog.Handler {
	handler := h.clone()
	handler.Handler = handler.Handler.WithGroup(name)
	return handler
}

func (h *Handler) clone() *Handler {
	return &Handler{
		Handler:           h.Handler,
		writer:            h.writer,
		level:             h.level,
		stacktraceEnabled: h.stacktraceEnabled,
		stacktraceLevel:   h.stacktraceLevel,
	}
}

func replaceCommonKeyValues(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.MessageKey {
		a.Key = "message"
		return a
	}
	if a.Key == slog.TimeKey {
		v := a.Value
		t := v.Time().Format(time.RFC3339)
		a.Value = slog.StringValue(t)
	}
	return a
}
