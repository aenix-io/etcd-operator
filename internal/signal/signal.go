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

package signal

import (
	"context"
	"os"
	"os/signal"
)

// NotifyContext returns a derived context from the parent context with a cancel
// function. It sets up a signal channel to listen for the specified signals and
// cancels the context when any of those signals are received. If a second signal
// is received, it exits the program with a status code of 1.
//
// Example usage:
//
//	ctx := signal.NotifyContext(parentContext, os.Interrupt, syscall.SIGTERM)
//	go func() {
//	    <-ctx.Done()
//	    // Perform cleanup or other tasks before exiting
//	}()
func NotifyContext(parent context.Context, signals ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(parent)
	c := make(chan os.Signal, 2)
	signal.Notify(c, signals...)
	go func() {
		<-c
		cancel()
		<-c
		os.Exit(1)
	}()
	return ctx
}
