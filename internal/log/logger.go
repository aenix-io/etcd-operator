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
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

func mapLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

type Parameters struct {
	LogLevel        string
	StacktraceLevel string
	Development     bool
}

func Setup(ctx context.Context, p Parameters) context.Context {
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:     "message",
		LevelKey:       "level",
		TimeKey:        "time",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}
	encoder := zapcore.NewJSONEncoder(encoderConfig)
	writer := os.Stderr
	if p.Development {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
		writer = os.Stdout
	}

	core := zapcore.NewCore(encoder, writer, mapLogLevel(p.LogLevel))
	logger := zap.New(core, zap.AddStacktrace(mapLogLevel(p.StacktraceLevel)))
	l := slog.New(zapslog.NewHandler(logger.Core(), &zapslog.HandlerOptions{AddSource: p.Development}))
	return logr.NewContextWithSlogLogger(ctx, l)
}

func Info(ctx context.Context, msg string, keysAndValues ...interface{}) {
	logr.FromContextAsSlogLogger(ctx).With(keysAndValues...).Info(msg)
}

func Debug(ctx context.Context, msg string, keysAndValues ...interface{}) {
	logr.FromContextAsSlogLogger(ctx).With(keysAndValues...).Debug(msg)
}

func Warn(ctx context.Context, msg string, keysAndValues ...interface{}) {
	logr.FromContextAsSlogLogger(ctx).With(keysAndValues...).Warn(msg)
}

func Error(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
	logr.FromContextAsSlogLogger(ctx).With(keysAndValues...).Error(msg, slog.Any("error", err))
}

func WithValues(ctx context.Context, keysAndValues ...interface{}) context.Context {
	return logr.NewContextWithSlogLogger(ctx, logr.FromContextAsSlogLogger(ctx).With(keysAndValues...))
}
