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
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	// EC2 Instance Metadata is configurable mainly for testing purposes
	instanceMetadataURLConfigKey            = "INSTANCE_METADATA_URL"
	defaultInstanceMetadataURL              = "http://169.254.169.254"
	dryRunConfigKey                         = "DRY_RUN"
	nodeNameConfigKey                       = "NODE_NAME"
	podNameConfigKey                        = "POD_NAME"
	podNamespaceConfigKey                   = "NAMESPACE"
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
	webhookTemplateDefault                  = `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Instance: {{ .InstanceID }} - Node: {{ .NodeName }} - Description: {{ .Description }} - Start Time: {{ .StartTime }}"}`
	enableScheduledEventDrainingConfigKey   = "ENABLE_SCHEDULED_EVENT_DRAINING"
	enableScheduledEventDrainingDefault     = true
	enableSpotInterruptionDrainingConfigKey = "ENABLE_SPOT_INTERRUPTION_DRAINING"
	enableSpotInterruptionDrainingDefault   = true
	enableASGLifecycleDrainingConfigKey     = "ENABLE_ASG_LIFECYCLE_DRAINING"
	enableASGLifecycleDrainingDefault       = false
	enableSQSTerminationDrainingConfigKey   = "ENABLE_SQS_TERMINATION_DRAINING"
	enableSQSTerminationDrainingDefault     = false
	enableRebalanceMonitoringConfigKey      = "ENABLE_REBALANCE_MONITORING"
	enableRebalanceMonitoringDefault        = false
	enableRebalanceDrainingConfigKey        = "ENABLE_REBALANCE_DRAINING"
	enableRebalanceDrainingDefault          = false
	checkASGTagBeforeDrainingConfigKey      = "CHECK_ASG_TAG_BEFORE_DRAINING"
	checkASGTagBeforeDrainingDefault        = true
	checkTagBeforeDrainingConfigKey         = "CHECK_TAG_BEFORE_DRAINING"
	checkTagBeforeDrainingDefault           = true
	managedAsgTagConfigKey                  = "MANAGED_ASG_TAG"
	managedTagConfigKey                     = "MANAGED_TAG"
	managedAsgTagDefault                    = "aws-node-termination-handler/managed"
	managedTagDefault                       = "aws-node-termination-handler/managed"
	useProviderIdConfigKey                  = "USE_PROVIDER_ID"
	useProviderIdDefault                    = false
	metadataTriesConfigKey                  = "METADATA_TRIES"
	metadataTriesDefault                    = 3
	cordonOnly                              = "CORDON_ONLY"
	taintNode                               = "TAINT_NODE"
	taintEffectDefault                      = "NoSchedule"
	taintEffect                             = "TAINT_EFFECT"
	enableOutOfServiceTaintConfigKey        = "ENABLE_OUT_OF_SERVICE_TAINT"
	enableOutOfServiceTaintDefault          = false
	excludeFromLoadBalancers                = "EXCLUDE_FROM_LOAD_BALANCERS"
	jsonLoggingConfigKey                    = "JSON_LOGGING"
	jsonLoggingDefault                      = false
	logLevelConfigKey                       = "LOG_LEVEL"
	logLevelDefault                         = "INFO"
	logFormatVersionKey                     = "LOG_FORMAT_VERSION"
	logFormatVersionDefault                 = 1
	MinSupportedLogFormatVersion            = 1
	MaxSupportedLogFormatVersion            = 2
	uptimeFromFileConfigKey                 = "UPTIME_FROM_FILE"
	uptimeFromFileDefault                   = ""
	workersConfigKey                        = "WORKERS"
	workersDefault                          = 10
	useAPIServerCache                       = "USE_APISERVER_CACHE"
	// prometheus
	enablePrometheusDefault   = false
	enablePrometheusConfigKey = "ENABLE_PROMETHEUS_SERVER"
	// https://github.com/prometheus/prometheus/wiki/Default-port-allocations
	prometheusPortDefault   = 9092
	prometheusPortConfigKey = "PROMETHEUS_SERVER_PORT"
	// probes
	enableProbesDefault                       = false
	enableProbesConfigKey                     = "ENABLE_PROBES_SERVER"
	probesPortDefault                         = 8080
	probesPortConfigKey                       = "PROBES_SERVER_PORT"
	probesEndpointDefault                     = "/healthz"
	probesEndpointConfigKey                   = "PROBES_SERVER_ENDPOINT"
	emitKubernetesEventsConfigKey             = "EMIT_KUBERNETES_EVENTS"
	emitKubernetesEventsDefault               = false
	kubernetesEventsExtraAnnotationsConfigKey = "KUBERNETES_EVENTS_EXTRA_ANNOTATIONS"
	awsRegionConfigKey                        = "AWS_REGION"
	awsEndpointConfigKey                      = "AWS_ENDPOINT"
	queueURLConfigKey                         = "QUEUE_URL"
	completeLifecycleActionDelaySecondsKey    = "COMPLETE_LIFECYCLE_ACTION_DELAY_SECONDS"
	deleteSqsMsgIfNodeNotFoundKey             = "DELETE_SQS_MSG_IF_NODE_NOT_FOUND"
	// heartbeat
	heartbeatIntervalKey = "HEARTBEAT_INTERVAL"
	heartbeatUntilKey    = "HEARTBEAT_UNTIL"
)

// Config arguments set via CLI, environment variables, or defaults
type Config struct {
	DryRun                              bool
	NodeName                            string
	PodName                             string
	PodNamespace                        string
	MetadataURL                         string
	IgnoreDaemonSets                    bool
	DeleteLocalData                     bool
	KubernetesServiceHost               string
	KubernetesServicePort               string
	PodTerminationGracePeriod           int
	NodeTerminationGracePeriod          int
	WebhookURL                          string
	WebhookHeaders                      string
	WebhookTemplate                     string
	WebhookTemplateFile                 string
	WebhookProxy                        string
	EnableScheduledEventDraining        bool
	EnableSpotInterruptionDraining      bool
	EnableASGLifecycleDraining          bool
	EnableSQSTerminationDraining        bool
	EnableRebalanceMonitoring           bool
	EnableRebalanceDraining             bool
	CheckASGTagBeforeDraining           bool
	CheckTagBeforeDraining              bool
	ManagedAsgTag                       string
	ManagedTag                          string
	MetadataTries                       int
	CordonOnly                          bool
	TaintNode                           bool
	TaintEffect                         string
	EnableOutOfServiceTaint             bool
	ExcludeFromLoadBalancers            bool
	JsonLogging                         bool
	LogLevel                            string
	LogFormatVersion                    int
	UptimeFromFile                      string
	EnablePrometheus                    bool
	PrometheusPort                      int
	EnableProbes                        bool
	ProbesPort                          int
	ProbesEndpoint                      string
	EmitKubernetesEvents                bool
	KubernetesEventsExtraAnnotations    string
	AWSRegion                           string
	AWSEndpoint                         string
	QueueURL                            string
	Workers                             int
	UseProviderId                       bool
	CompleteLifecycleActionDelaySeconds int
	DeleteSqsMsgIfNodeNotFound          bool
	UseAPIServerCacheToListPods         bool
	HeartbeatInterval                   int
	HeartbeatUntil                      int
}

// ParseCliArgs parses cli arguments and uses environment variables as fallback values
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
	flag.StringVar(&config.PodName, "pod-name", getEnv(podNameConfigKey, ""), "The kubernetes pod name")
	flag.StringVar(&config.PodNamespace, "pod-namespace", getEnv(podNamespaceConfigKey, ""), "The kubernetes pod namespace")
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
	flag.BoolVar(&config.EnableScheduledEventDraining, "enable-scheduled-event-draining", getBoolEnv(enableScheduledEventDrainingConfigKey, enableScheduledEventDrainingDefault), "If true, drain nodes before the maintenance window starts for an EC2 instance scheduled event")
	flag.BoolVar(&config.EnableSpotInterruptionDraining, "enable-spot-interruption-draining", getBoolEnv(enableSpotInterruptionDrainingConfigKey, enableSpotInterruptionDrainingDefault), "If true, drain nodes when the spot interruption termination notice is received")
	flag.BoolVar(&config.EnableASGLifecycleDraining, "enable-asg-lifecycle-draining", getBoolEnv(enableASGLifecycleDrainingConfigKey, enableASGLifecycleDrainingDefault), "If true, drain nodes when the ASG target lifecyle state is Terminated is received")
	flag.BoolVar(&config.EnableSQSTerminationDraining, "enable-sqs-termination-draining", getBoolEnv(enableSQSTerminationDrainingConfigKey, enableSQSTerminationDrainingDefault), "If true, drain nodes when an SQS termination event is received")
	flag.BoolVar(&config.EnableRebalanceMonitoring, "enable-rebalance-monitoring", getBoolEnv(enableRebalanceMonitoringConfigKey, enableRebalanceMonitoringDefault), "If true, cordon nodes when the rebalance recommendation notice is received. If you'd like to drain the node in addition to cordoning, then also set \"enableRebalanceDraining\".")
	flag.BoolVar(&config.EnableRebalanceDraining, "enable-rebalance-draining", getBoolEnv(enableRebalanceDrainingConfigKey, enableRebalanceDrainingDefault), "If true, drain nodes when the rebalance recommendation notice is received")
	flag.BoolVar(&config.CheckASGTagBeforeDraining, "check-asg-tag-before-draining", getBoolEnv(checkASGTagBeforeDrainingConfigKey, checkASGTagBeforeDrainingDefault), "[DEPRECATED] * Use check-tag-before-draining instead * If true, check that the instance is tagged with \"aws-node-termination-handler/managed\" as the key before draining the node. If false, disables calls to ASG API.")
	flag.BoolVar(&config.CheckTagBeforeDraining, "check-tag-before-draining", getBoolEnv(checkTagBeforeDrainingConfigKey, checkTagBeforeDrainingDefault), "If true, check that the instance is tagged with \"aws-node-termination-handler/managed\" as the key before draining the node.")
	flag.StringVar(&config.ManagedAsgTag, "managed-asg-tag", getEnv(managedAsgTagConfigKey, managedAsgTagDefault), "[DEPRECATED] * Use managed-tag instead * Sets the tag to check instances for that is propogated from the ASG before taking action, default to aws-node-termination-handler/managed")
	flag.StringVar(&config.ManagedTag, "managed-tag", getEnv(managedTagConfigKey, managedTagDefault), "Sets the tag to check instances for before taking action, default to aws-node-termination-handler/managed")
	flag.IntVar(&config.MetadataTries, "metadata-tries", getIntEnv(metadataTriesConfigKey, metadataTriesDefault), "The number of times to try requesting metadata. If you would like 2 retries, set metadata-tries to 3.")
	flag.BoolVar(&config.CordonOnly, "cordon-only", getBoolEnv(cordonOnly, false), "If true, nodes will be cordoned but not drained when an interruption event occurs.")
	flag.BoolVar(&config.TaintNode, "taint-node", getBoolEnv(taintNode, false), "If true, nodes will be tainted when an interruption event occurs.")
	flag.StringVar(&config.TaintEffect, "taint-effect", getEnv(taintEffect, taintEffectDefault), "Sets the effect when a node is tainted.")
	flag.BoolVar(&config.EnableOutOfServiceTaint, "enable-out-of-service-taint", getBoolEnv(enableOutOfServiceTaintConfigKey, enableOutOfServiceTaintDefault), "If true, nodes will be tainted as out-of-service after we cordon/drain the nodes when an interruption event occurs.")
	flag.BoolVar(&config.ExcludeFromLoadBalancers, "exclude-from-load-balancers", getBoolEnv(excludeFromLoadBalancers, false), "If true, nodes will be marked for exclusion from load balancers when an interruption event occurs.")
	flag.BoolVar(&config.JsonLogging, "json-logging", getBoolEnv(jsonLoggingConfigKey, jsonLoggingDefault), "If true, use JSON-formatted logs instead of human readable logs.")
	flag.StringVar(&config.LogLevel, "log-level", getEnv(logLevelConfigKey, logLevelDefault), "Sets the log level (INFO, DEBUG, or ERROR)")
	flag.IntVar(&config.LogFormatVersion, "log-format-version", getIntEnv(logFormatVersionKey, logFormatVersionDefault), "Sets the log format version.")
	flag.StringVar(&config.UptimeFromFile, "uptime-from-file", getEnv(uptimeFromFileConfigKey, uptimeFromFileDefault), "If specified, read system uptime from the file path (useful for testing).")
	flag.BoolVar(&config.EnablePrometheus, "enable-prometheus-server", getBoolEnv(enablePrometheusConfigKey, enablePrometheusDefault), "If true, a http server is used for exposing prometheus metrics in /metrics endpoint.")
	flag.IntVar(&config.PrometheusPort, "prometheus-server-port", getIntEnv(prometheusPortConfigKey, prometheusPortDefault), "The port for running the prometheus http server.")
	flag.BoolVar(&config.EnableProbes, "enable-probes-server", getBoolEnv(enableProbesConfigKey, enableProbesDefault), "If true, a http server is used for exposing probes in /healthz endpoint.")
	flag.IntVar(&config.ProbesPort, "probes-server-port", getIntEnv(probesPortConfigKey, probesPortDefault), "The port for running the probes http server.")
	flag.StringVar(&config.ProbesEndpoint, "probes-server-endpoint", getEnv(probesEndpointConfigKey, probesEndpointDefault), "If specified, use this endpoint to make liveness probe")
	flag.BoolVar(&config.EmitKubernetesEvents, "emit-kubernetes-events", getBoolEnv(emitKubernetesEventsConfigKey, emitKubernetesEventsDefault), "If true, Kubernetes events will be emitted when interruption events are received and when actions are taken on Kubernetes nodes")
	flag.StringVar(&config.KubernetesEventsExtraAnnotations, "kubernetes-events-extra-annotations", getEnv(kubernetesEventsExtraAnnotationsConfigKey, ""), "A comma-separated list of key=value extra annotations to attach to all emitted Kubernetes events. Example: --kubernetes-events-extra-annotations first=annotation,sample.annotation/number=two")
	flag.StringVar(&config.AWSRegion, "aws-region", getEnv(awsRegionConfigKey, ""), "If specified, use the AWS region for AWS API calls")
	flag.StringVar(&config.AWSEndpoint, "aws-endpoint", getEnv(awsEndpointConfigKey, ""), "[testing] If specified, use the AWS endpoint to make API calls")
	flag.StringVar(&config.QueueURL, "queue-url", getEnv(queueURLConfigKey, ""), "Listens for messages on the specified SQS queue URL")
	flag.IntVar(&config.Workers, "workers", getIntEnv(workersConfigKey, workersDefault), "The amount of parallel event processors.")
	flag.BoolVar(&config.UseProviderId, "use-provider-id", getBoolEnv(useProviderIdConfigKey, useProviderIdDefault), "If true, fetch node name through Kubernetes node spec ProviderID instead of AWS event PrivateDnsHostname.")
	flag.IntVar(&config.CompleteLifecycleActionDelaySeconds, "complete-lifecycle-action-delay-seconds", getIntEnv(completeLifecycleActionDelaySecondsKey, -1), "Delay completing the Autoscaling lifecycle action after a node has been drained.")
	flag.BoolVar(&config.DeleteSqsMsgIfNodeNotFound, "delete-sqs-msg-if-node-not-found", getBoolEnv(deleteSqsMsgIfNodeNotFoundKey, false), "If true, delete SQS Messages from the SQS Queue if the targeted node(s) are not found.")
	flag.BoolVar(&config.UseAPIServerCacheToListPods, "use-apiserver-cache", getBoolEnv(useAPIServerCache, false), "If true, leverage the k8s apiserver's index on pod's spec.nodeName to list pods on a node, instead of doing an etcd quorum read.")
	flag.IntVar(&config.HeartbeatInterval, "heartbeat-interval", getIntEnv(heartbeatIntervalKey, -1), "The time period in seconds between consecutive heartbeat signals. Valid range: 30-3600 seconds (30 seconds to 1 hour).")
	flag.IntVar(&config.HeartbeatUntil, "heartbeat-until", getIntEnv(heartbeatUntilKey, -1), "The duration in seconds over which heartbeat signals are sent. Valid range: 60-172800 seconds (1 minute to 48 hours).")
	flag.Parse()

	if isConfigProvided("pod-termination-grace-period", podTerminationGracePeriodConfigKey) && isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Warn().Msg("Deprecated argument \"grace-period\" and the replacement argument \"pod-termination-grace-period\" was provided. Using the newer argument \"pod-termination-grace-period\"")
	} else if isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Warn().Msg("Deprecated argument \"grace-period\" was provided. This argument will eventually be removed. Please switch to \"pod-termination-grace-period\" instead.")
		config.PodTerminationGracePeriod = gracePeriod
	}

	if isConfigProvided("managed-asg-tag", managedAsgTagConfigKey) && isConfigProvided("managed-tag", managedTagConfigKey) {
		log.Warn().Msg("Deprecated argument \"managed-asg-tag\" and the replacement argument \"managed-tag\" was provided. Using the newer argument \"managed-tag\"")
	} else if isConfigProvided("managed-asg-tag", managedAsgTagConfigKey) {
		log.Warn().Msg("Deprecated argument \"managed-asg-tag\" was provided. This argument will eventually be removed. Please switch to \"managed-tag\" instead.")
		config.ManagedTag = config.ManagedAsgTag
	}

	if isConfigProvided("check-asg-tag-before-draining", checkASGTagBeforeDrainingConfigKey) && isConfigProvided("check-tag-before-draining", checkTagBeforeDrainingConfigKey) {
		log.Warn().Msg("Deprecated argument \"check-asg-tag-before-draining\" and the replacement argument \"check-tag-before-draining\" was provided. Using the newer argument \"check-tag-before-draining\"")
	} else if isConfigProvided("check-asg-tag-before-draining", checkASGTagBeforeDrainingConfigKey) {
		log.Warn().Msg("Deprecated argument \"check-asg-tag-before-draining\" was provided. This argument will eventually be removed. Please switch to \"check-tag-before-draining\" instead.")
		config.CheckTagBeforeDraining = config.CheckASGTagBeforeDraining
	}

	switch strings.ToLower(config.LogLevel) {
	case "info":
	case "debug":
	case "error":
	default:
		return config, fmt.Errorf("invalid log-level passed: %s  Should be one of: info, debug, error", config.LogLevel)
	}

	if config.LogFormatVersion < MinSupportedLogFormatVersion {
		log.Warn().Msgf("Log format version %d is not supported, using format version %d", config.LogFormatVersion, MinSupportedLogFormatVersion)
		config.LogFormatVersion = MinSupportedLogFormatVersion
	}
	if config.LogFormatVersion > MaxSupportedLogFormatVersion {
		log.Warn().Msgf("Log format version %d is not supported, using format version %d", config.LogFormatVersion, MaxSupportedLogFormatVersion)
		config.LogFormatVersion = MaxSupportedLogFormatVersion
	}

	if config.NodeName == "" {
		panic("You must provide a node-name to the CLI or NODE_NAME environment variable.")
	}

	// heartbeat value boundary and compability check
	if !config.EnableSQSTerminationDraining && (config.HeartbeatInterval != -1 || config.HeartbeatUntil != -1) {
		return config, fmt.Errorf("currently using IMDS mode. Heartbeat is only supported for Queue Processor mode")
	}
	if config.HeartbeatInterval != -1 && (config.HeartbeatInterval < 30 || config.HeartbeatInterval > 3600) {
		return config, fmt.Errorf("invalid heartbeat-interval passed: %d  Should be between 30 and 3600 seconds", config.HeartbeatInterval)
	}
	if config.HeartbeatUntil != -1 && (config.HeartbeatUntil < 60 || config.HeartbeatUntil > 172800) {
		return config, fmt.Errorf("invalid heartbeat-until passed: %d  Should be between 60 and 172800 seconds", config.HeartbeatUntil)
	}
	if config.HeartbeatInterval == -1 && config.HeartbeatUntil != -1 {
		return config, fmt.Errorf("invalid heartbeat configuration: heartbeat-interval is required when heartbeat-until is set")
	}
	if config.HeartbeatInterval != -1 && config.HeartbeatUntil == -1 {
		config.HeartbeatUntil = 172800
		log.Info().Msgf("Since heartbeat-until is not set, defaulting to %d seconds", config.HeartbeatUntil)
	}
	if config.HeartbeatInterval != -1 && config.HeartbeatUntil != -1 && config.HeartbeatInterval > config.HeartbeatUntil {
		return config, fmt.Errorf("invalid heartbeat configuration: heartbeat-interval should be less than or equal to heartbeat-until")
	}

	// client-go expects these to be set in env vars
	os.Setenv(kubernetesServiceHostConfigKey, config.KubernetesServiceHost)
	os.Setenv(kubernetesServicePortConfigKey, config.KubernetesServicePort)

	return config, err
}

// Print uses the JSON log setting to print either JSON formatted config value logs or human-readable config values
func (c Config) Print() {
	if c.JsonLogging {
		c.PrintJsonConfigArgs()
	} else {
		c.PrintHumanConfigArgs()
	}
}

// PrintJsonConfigArgs prints the config values with JSON formatting
func (c Config) PrintJsonConfigArgs() {
	// manually setting fields instead of using log.Log().Interface() to use snake_case instead of PascalCase
	// intentionally did not log webhook configuration as there may be secrets
	log.Info().
		Bool("dry_run", c.DryRun).
		Str("node_name", c.NodeName).
		Str("pod_name", c.PodName).
		Str("pod_namespace", c.PodNamespace).
		Str("metadata_url", c.MetadataURL).
		Str("kubernetes_service_host", c.KubernetesServiceHost).
		Str("kubernetes_service_port", c.KubernetesServicePort).
		Bool("delete_local_data", c.DeleteLocalData).
		Bool("ignore_daemon_sets", c.IgnoreDaemonSets).
		Int("pod_termination_grace_period", c.PodTerminationGracePeriod).
		Int("node_termination_grace_period", c.NodeTerminationGracePeriod).
		Bool("enable_scheduled_event_draining", c.EnableScheduledEventDraining).
		Bool("enable_spot_interruption_draining", c.EnableSpotInterruptionDraining).
		Bool("enable_sqs_termination_draining", c.EnableSQSTerminationDraining).
		Bool("delete_sqs_msg_if_node_not_found", c.DeleteSqsMsgIfNodeNotFound).
		Bool("enable_rebalance_monitoring", c.EnableRebalanceMonitoring).
		Bool("enable_rebalance_draining", c.EnableRebalanceDraining).
		Int("metadata_tries", c.MetadataTries).
		Bool("cordon_only", c.CordonOnly).
		Bool("taint_node", c.TaintNode).
		Str("taint_effect", c.TaintEffect).
		Bool("enable_out_of_service_taint", c.EnableOutOfServiceTaint).
		Bool("exclude_from_load_balancers", c.ExcludeFromLoadBalancers).
		Bool("json_logging", c.JsonLogging).
		Str("log_level", c.LogLevel).
		Str("webhook_proxy", c.WebhookProxy).
		Str("uptime_from_file", c.UptimeFromFile).
		Bool("enable_prometheus_server", c.EnablePrometheus).
		Int("prometheus_server_port", c.PrometheusPort).
		Bool("emit_kubernetes_events", c.EmitKubernetesEvents).
		Str("kubernetes_events_extra_annotations", c.KubernetesEventsExtraAnnotations).
		Str("aws_region", c.AWSRegion).
		Str("aws_endpoint", c.AWSEndpoint).
		Str("queue_url", c.QueueURL).
		Bool("check_tag_before_draining", c.CheckTagBeforeDraining).
		Str("ManagedTag", c.ManagedTag).
		Bool("use_provider_id", c.UseProviderId).
		Bool("use_apiserver_cache", c.UseAPIServerCacheToListPods).
		Int("heartbeat_interval", c.HeartbeatInterval).
		Int("heartbeat_until", c.HeartbeatUntil).
		Msg("aws-node-termination-handler arguments")
}

// PrintHumanConfigArgs prints config args as a human-reable pretty printed string
func (c Config) PrintHumanConfigArgs() {
	webhookURLDisplay := ""
	if c.WebhookURL != "" {
		webhookURLDisplay = "<provided-not-displayed>"
	}
	// intentionally did not log webhook configuration as there may be secrets
	log.Info().Msgf(
		"aws-node-termination-handler arguments: \n"+
			"\tdry-run: %t,\n"+
			"\tnode-name: %s,\n"+
			"\tpod-name: %s,\n"+
			"\tpod-namespace: %s,\n"+
			"\tmetadata-url: %s,\n"+
			"\tkubernetes-service-host: %s,\n"+
			"\tkubernetes-service-port: %s,\n"+
			"\tdelete-local-data: %t,\n"+
			"\tignore-daemon-sets: %t,\n"+
			"\tpod-termination-grace-period: %d,\n"+
			"\tnode-termination-grace-period: %d,\n"+
			"\tenable-scheduled-event-draining: %t,\n"+
			"\tenable-spot-interruption-draining: %t,\n"+
			"\tenable-sqs-termination-draining: %t,\n"+
			"\tdelete-sqs-msg-if-node-not-found: %t,\n"+
			"\tenable-rebalance-monitoring: %t,\n"+
			"\tenable-rebalance-draining: %t,\n"+
			"\tmetadata-tries: %d,\n"+
			"\tcordon-only: %t,\n"+
			"\ttaint-node: %t,\n"+
			"\ttaint-effect: %s,\n"+
			"\tenable-out-of-service-taint: %t,\n"+
			"\texclude-from-load-balancers: %t,\n"+
			"\tjson-logging: %t,\n"+
			"\tlog-level: %s,\n"+
			"\twebhook-proxy: %s,\n"+
			"\twebhook-headers: %s,\n"+
			"\twebhook-url: %s,\n"+
			"\twebhook-template: %s,\n"+
			"\tuptime-from-file: %s,\n"+
			"\tenable-prometheus-server: %t,\n"+
			"\tprometheus-server-port: %d,\n"+
			"\temit-kubernetes-events: %t,\n"+
			"\tkubernetes-events-extra-annotations: %s,\n"+
			"\taws-region: %s,\n"+
			"\tqueue-url: %s,\n"+
			"\tcheck-tag-before-draining: %t,\n"+
			"\tmanaged-tag: %s,\n"+
			"\tuse-provider-id: %t,\n"+
			"\taws-endpoint: %s,\n"+
			"\tuse-apiserver-cache: %t,\n"+
			"\theartbeat-interval: %d,\n"+
			"\theartbeat-until: %d\n",
		c.DryRun,
		c.NodeName,
		c.PodName,
		c.PodNamespace,
		c.MetadataURL,
		c.KubernetesServiceHost,
		c.KubernetesServicePort,
		c.DeleteLocalData,
		c.IgnoreDaemonSets,
		c.PodTerminationGracePeriod,
		c.NodeTerminationGracePeriod,
		c.EnableScheduledEventDraining,
		c.EnableSpotInterruptionDraining,
		c.EnableSQSTerminationDraining,
		c.DeleteSqsMsgIfNodeNotFound,
		c.EnableRebalanceMonitoring,
		c.EnableRebalanceDraining,
		c.MetadataTries,
		c.CordonOnly,
		c.TaintNode,
		c.TaintEffect,
		c.EnableOutOfServiceTaint,
		c.ExcludeFromLoadBalancers,
		c.JsonLogging,
		c.LogLevel,
		c.WebhookProxy,
		"<not-displayed>",
		webhookURLDisplay,
		"<not-displayed>",
		c.UptimeFromFile,
		c.EnablePrometheus,
		c.PrometheusPort,
		c.EmitKubernetesEvents,
		c.KubernetesEventsExtraAnnotations,
		c.AWSRegion,
		c.QueueURL,
		c.CheckTagBeforeDraining,
		c.ManagedTag,
		c.UseProviderId,
		c.AWSEndpoint,
		c.UseAPIServerCacheToListPods,
		c.HeartbeatInterval,
		c.HeartbeatUntil,
	)
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
