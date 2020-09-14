// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/rs/zerolog/log"
)

const (
	// EC2 Instance Metadata is configurable mainly for testing purposes
	instanceMetadataURLConfigKey            = "INSTANCE_METADATA_URL"
	defaultInstanceMetadataURL              = "http://169.254.169.254"
	dryRunConfigKey                         = "DRY_RUN"
	nodeNameConfigKey                       = "NODE_NAME"
	kubernetesServiceHostConfigKey          = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePortConfigKey          = "KUBERNETES_SERVICE_PORT"
	deleteLocalDataConfigKey                = "DELETE_LOCAL_DATA"
	ignoreDaemonSetsConfigKey               = "IGNORE_DAEMON_SETS"
	gracePeriodConfigKey                    = "GRACE_PERIOD"
	podTerminationGracePeriodConfigKey      = "POD_TERMINATION_GRACE_PERIOD"
	podTerminationGracePeriodDefault        = -1
	nodeTerminationGracePeriodConfigKey     = "NODE_TERMINATION_GRACE_PERIOD"
	nodeTerminationGracePeriodDefault       = 120
	webhookURLConfigKey                     = "WEBHOOK_URL"
	webhookURLDefault                       = ""
	webhookProxyConfigKey                   = "WEBHOOK_PROXY"
	webhookProxyDefault                     = ""
	webhookHeadersConfigKey                 = "WEBHOOK_HEADERS"
	webhookHeadersDefault                   = `{"Content-type":"application/json"}`
	webhookTemplateConfigKey                = "WEBHOOK_TEMPLATE"
	webhookTemplateFileConfigKey            = "WEBHOOK_TEMPLATE_FILE"
	webhookTemplateDefault                  = `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Description: {{ .Description }} - Start Time: {{ .StartTime }}"}`
	enableScheduledEventDrainingConfigKey   = "ENABLE_SCHEDULED_EVENT_DRAINING"
	enableScheduledEventDrainingDefault     = false
	enableSpotInterruptionDrainingConfigKey = "ENABLE_SPOT_INTERRUPTION_DRAINING"
	enableSpotInterruptionDrainingDefault   = true
	metadataTriesConfigKey                  = "METADATA_TRIES"
	metadataTriesDefault                    = 3
	cordonOnly                              = "CORDON_ONLY"
	taintNode                               = "TAINT_NODE"
	jsonLoggingConfigKey                    = "JSON_LOGGING"
	jsonLoggingDefault                      = false
	uptimeFromFileConfigKey                 = "UPTIME_FROM_FILE"
	uptimeFromFileDefault                   = ""
	// prometheus
	enablePrometheusDefault   = false
	enablePrometheusConfigKey = "ENABLE_PROMETHEUS_SERVER"
	// https://github.com/prometheus/prometheus/wiki/Default-port-allocations
	prometheusPortDefault   = 9092
	prometheusPortConfigKey = "PROMETHEUS_SERVER_PORT"
)

//Config arguments set via CLI, environment variables, or defaults
type Config struct {
	DryRun                         bool
	NodeName                       string
	MetadataURL                    string
	IgnoreDaemonSets               bool
	DeleteLocalData                bool
	KubernetesServiceHost          string
	KubernetesServicePort          string
	PodTerminationGracePeriod      int
	NodeTerminationGracePeriod     int
	WebhookURL                     string
	WebhookHeaders                 string
	WebhookTemplate                string
	WebhookTemplateFile            string
	WebhookProxy                   string
	EnableScheduledEventDraining   bool
	EnableSpotInterruptionDraining bool
	MetadataTries                  int
	CordonOnly                     bool
	TaintNode                      bool
	JsonLogging                    bool
	UptimeFromFile                 string
	EnablePrometheus               bool
	PrometheusPort                 int
}

//ParseCliArgs parses cli arguments and uses environment variables as fallback values
func ParseCliArgs() (config Config, err error) {
	var gracePeriod int
	defer func() {
		if r := recover(); r != nil {
			switch pval := r.(type) {
			default:
				err = fmt.Errorf("%v", pval)
			}
		}
	}()
	flag.BoolVar(&config.DryRun, "dry-run", getBoolEnv(dryRunConfigKey, false), "If true, only log if a node would be drained")
	flag.StringVar(&config.NodeName, "node-name", getEnv(nodeNameConfigKey, ""), "The kubernetes node name")
	flag.StringVar(&config.MetadataURL, "metadata-url", getEnv(instanceMetadataURLConfigKey, defaultInstanceMetadataURL), "The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing.")
	flag.BoolVar(&config.IgnoreDaemonSets, "ignore-daemon-sets", getBoolEnv(ignoreDaemonSetsConfigKey, true), "If true, ignore daemon sets and drain other pods when a spot interrupt is received.")
	flag.BoolVar(&config.DeleteLocalData, "delete-local-data", getBoolEnv(deleteLocalDataConfigKey, true), "If true, do not drain pods that are using local node storage in emptyDir")
	flag.StringVar(&config.KubernetesServiceHost, "kubernetes-service-host", getEnv(kubernetesServiceHostConfigKey, ""), "[ADVANCED] The k8s service host to send api calls to.")
	flag.StringVar(&config.KubernetesServicePort, "kubernetes-service-port", getEnv(kubernetesServicePortConfigKey, ""), "[ADVANCED] The k8s service port to send api calls to.")
	flag.IntVar(&gracePeriod, "grace-period", getIntEnv(gracePeriodConfigKey, podTerminationGracePeriodDefault), "[DEPRECATED] * Use pod-termination-grace-period instead * Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.")
	flag.IntVar(&config.PodTerminationGracePeriod, "pod-termination-grace-period", getIntEnv(podTerminationGracePeriodConfigKey, podTerminationGracePeriodDefault), "Period of time in seconds given to each POD to terminate gracefully. If negative, the default value specified in the pod will be used.")
	flag.IntVar(&config.NodeTerminationGracePeriod, "node-termination-grace-period", getIntEnv(nodeTerminationGracePeriodConfigKey, nodeTerminationGracePeriodDefault), "Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled based on this value to optimize the amount of compute time, but still safely drain the node before an event.")
	flag.StringVar(&config.WebhookURL, "webhook-url", getEnv(webhookURLConfigKey, webhookURLDefault), "If specified, posts event data to URL upon instance interruption action.")
	flag.StringVar(&config.WebhookProxy, "webhook-proxy", getEnv(webhookProxyConfigKey, webhookProxyDefault), "If specified, uses the HTTP(S) proxy to send webhooks. Example: --webhook-url='tcp://<ip-or-dns-to-proxy>:<port>'")
	flag.StringVar(&config.WebhookHeaders, "webhook-headers", getEnv(webhookHeadersConfigKey, webhookHeadersDefault), "If specified, replaces the default webhook headers.")
	flag.StringVar(&config.WebhookTemplate, "webhook-template", getEnv(webhookTemplateConfigKey, webhookTemplateDefault), "If specified, replaces the default webhook message template.")
	flag.StringVar(&config.WebhookTemplateFile, "webhook-template-file", getEnv(webhookTemplateFileConfigKey, ""), "If specified, replaces the default webhook message template with content from template file.")
	flag.BoolVar(&config.EnableScheduledEventDraining, "enable-scheduled-event-draining", getBoolEnv(enableScheduledEventDrainingConfigKey, enableScheduledEventDrainingDefault), "[EXPERIMENTAL] If true, drain nodes before the maintenance window starts for an EC2 instance scheduled event")
	flag.BoolVar(&config.EnableSpotInterruptionDraining, "enable-spot-interruption-draining", getBoolEnv(enableSpotInterruptionDrainingConfigKey, enableSpotInterruptionDrainingDefault), "If false, do not drain nodes when the spot interruption termination notice is received")
	flag.IntVar(&config.MetadataTries, "metadata-tries", getIntEnv(metadataTriesConfigKey, metadataTriesDefault), "The number of times to try requesting metadata. If you would like 2 retries, set metadata-tries to 3.")
	flag.BoolVar(&config.CordonOnly, "cordon-only", getBoolEnv(cordonOnly, false), "If true, nodes will be cordoned but not drained when an interruption event occurs.")
	flag.BoolVar(&config.TaintNode, "taint-node", getBoolEnv(taintNode, false), "If true, nodes will be tainted when an interruption event occurs.")
	flag.BoolVar(&config.JsonLogging, "json-logging", getBoolEnv(jsonLoggingConfigKey, jsonLoggingDefault), "If true, use JSON-formatted logs instead of human readable logs.")
	flag.StringVar(&config.UptimeFromFile, "uptime-from-file", getEnv(uptimeFromFileConfigKey, uptimeFromFileDefault), "If specified, read system uptime from the file path (useful for testing).")
	flag.BoolVar(&config.EnablePrometheus, "enable-prometheus-server", getBoolEnv(enablePrometheusConfigKey, enablePrometheusDefault), "If true, a http server is used for exposing prometheus metrics in /metrics endpoint.")
	flag.IntVar(&config.PrometheusPort, "prometheus-server-port", getIntEnv(prometheusPortConfigKey, prometheusPortDefault), "The port for running the prometheus http server.")

	flag.Parse()

	if isConfigProvided("pod-termination-grace-period", podTerminationGracePeriodConfigKey) && isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Log().Msg("Deprecated argument \"grace-period\" and the replacement argument \"pod-termination-grace-period\" was provided. Using the newer argument \"pod-termination-grace-period\"")
	} else if isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Log().Msg("Deprecated argument \"grace-period\" was provided. This argument will eventually be removed. Please switch to \"pod-termination-grace-period\" instead.")
		config.PodTerminationGracePeriod = gracePeriod
	}

	if config.NodeName == "" {
		panic("You must provide a node-name to the CLI or NODE_NAME environment variable.")
	}

	// client-go expects these to be set in env vars
	os.Setenv(kubernetesServiceHostConfigKey, config.KubernetesServiceHost)
	os.Setenv(kubernetesServicePortConfigKey, config.KubernetesServicePort)

	return config, err
}

// Get env var or default
func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		if value != "" {
			return value
		}
	}
	return fallback
}

// Parse env var to int if key exists
func getIntEnv(key string, fallback int) int {
	envStrValue := getEnv(key, "")
	if envStrValue == "" {
		return fallback
	}
	envIntValue, err := strconv.Atoi(envStrValue)
	if err != nil {
		panic("Env Var " + key + " must be an integer")
	}
	return envIntValue
}

// Parse env var to boolean if key exists
func getBoolEnv(key string, fallback bool) bool {
	envStrValue := getEnv(key, "")
	if envStrValue == "" {
		return fallback
	}
	envBoolValue, err := strconv.ParseBool(envStrValue)
	if err != nil {
		panic("Env Var " + key + " must be either true or false")
	}
	return envBoolValue
}

func isConfigProvided(cliArgName string, envVarName string) bool {
	cliArgProvided := false
	if getEnv(envVarName, "") != "" {
		return true
	}
	flag.Visit(func(f *flag.Flag) {
		if f.Name == cliArgName {
			cliArgProvided = true
		}
	})
	return cliArgProvided
}
