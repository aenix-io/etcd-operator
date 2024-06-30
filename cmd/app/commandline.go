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

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aenix-io/etcd-operator/internal/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Flags struct {
	Kubeconfig       string
	MetricsAddress   string
	ProbeAddress     string
	LeaderElection   bool
	SecureMetrics    bool
	EnableHTTP2      bool
	DisableWebhooks  bool
	LogLevel         string
	StacktraceLevel  string
	EnableStacktrace bool
	Dev              bool
}

func ParseCmdLine() Flags {
	pflag.Usage = usage
	pflag.ErrHelp = nil
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)

	pflag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	pflag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	pflag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	pflag.Bool("leader-elect", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")
	pflag.Bool("metrics-secure", false, "If set the metrics endpoint is served securely.")
	pflag.Bool("enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers.")
	pflag.Bool("disable-webhooks", false, "If set, the webhooks will be disabled.")
	pflag.String("log-level", "info", "Logger verbosity level.Applicable values are debug, info, warn, error.")
	pflag.Bool("enable-stacktrace", true, "If set log entries will contain stacktrace.")
	pflag.String("stacktrace-level", "error", "Logger level to add stacktrace. "+
		"Applicable values are debug, info, warn, error.")
	pflag.Bool("dev", false, "development mode.")

	var help bool
	pflag.BoolVarP(&help, "help", "h", false, "Show this help message.")
	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		exitUsage(err)
	}
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	err = pflag.CommandLine.Parse(os.Args[1:])
	if err != nil {
		exitUsage(err)
	}
	if help {
		exitUsage(nil)
	}

	return Flags{
		Kubeconfig:       viper.GetString("kubeconfig"),
		MetricsAddress:   viper.GetString("metrics-bind-address"),
		ProbeAddress:     viper.GetString("health-probe-bind-address"),
		LeaderElection:   viper.GetBool("leader-elect"),
		SecureMetrics:    viper.GetBool("metrics-secure"),
		EnableHTTP2:      viper.GetBool("enable-http2"),
		DisableWebhooks:  viper.GetBool("disable-webhooks"),
		LogLevel:         viper.GetString("log-level"),
		StacktraceLevel:  viper.GetString("stacktrace-level"),
		EnableStacktrace: viper.GetBool("enable-stacktrace"),
		Dev:              viper.GetBool("dev"),
	}
}

func usage() {
	name := filepath.Base(os.Args[0])
	_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [--option]...\n", name)
	_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
	pflag.PrintDefaults()
}

func exitUsage(err error) {
	code := 0
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %s\n", filepath.Base(os.Args[0]), err)
		code = 2
	}
	pflag.Usage()
	os.Exit(code)
}

func LogParameters(flags Flags) log.Parameters {
	return log.Parameters{
		LogLevel:         flags.LogLevel,
		StacktraceLevel:  flags.StacktraceLevel,
		EnableStacktrace: flags.EnableStacktrace,
		Development:      flags.Dev,
	}
}
