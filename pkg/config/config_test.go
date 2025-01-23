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

package config_test

import (
	"bytes"
	"flag"
	"os"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func resetFlagsForTest() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"cmd"}
}

func TestParseCliArgsEnvSuccess(t *testing.T) {
	t.Setenv("USE_PROVIDER_ID", "true")
	t.Setenv("DELETE_LOCAL_DATA", "false")
	t.Setenv("DRY_RUN", "true")
	t.Setenv("ENABLE_SCHEDULED_EVENT_DRAINING", "true")
	t.Setenv("ENABLE_SPOT_INTERRUPTION_DRAINING", "false")
	t.Setenv("ENABLE_ASG_LIFECYCLE_DRAINING", "false")
	t.Setenv("ENABLE_SQS_TERMINATION_DRAINING", "true")
	t.Setenv("ENABLE_REBALANCE_MONITORING", "true")
	t.Setenv("ENABLE_REBALANCE_DRAINING", "true")
	t.Setenv("GRACE_PERIOD", "12345")
	t.Setenv("IGNORE_DAEMON_SETS", "false")
	t.Setenv("KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_HOST")
	t.Setenv("KUBERNETES_SERVICE_PORT", "KUBERNETES_SERVICE_PORT")
	t.Setenv("NODE_NAME", "NODE_NAME")
	t.Setenv("NODE_TERMINATION_GRACE_PERIOD", "12345")
	t.Setenv("INSTANCE_METADATA_URL", "INSTANCE_METADATA_URL")
	t.Setenv("POD_TERMINATION_GRACE_PERIOD", "12345")
	t.Setenv("WEBHOOK_URL", "WEBHOOK_URL")
	t.Setenv("WEBHOOK_HEADERS", "WEBHOOK_HEADERS")
	t.Setenv("WEBHOOK_TEMPLATE", "WEBHOOK_TEMPLATE")
	t.Setenv("METADATA_TRIES", "100")
	t.Setenv("CORDON_ONLY", "false")
	t.Setenv("USE_APISERVER_CACHE", "true")
	t.Setenv("HEARTBEAT_INTERVAL", "30")
	t.Setenv("HEARTBEAT_UNTIL", "60")
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, true, nthConfig.UseProviderId)
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
	h.Equals(t, false, nthConfig.EnableASGLifecycleDraining)
	h.Equals(t, true, nthConfig.EnableSQSTerminationDraining)
	h.Equals(t, true, nthConfig.EnableRebalanceMonitoring)
	h.Equals(t, true, nthConfig.EnableRebalanceDraining)
	h.Equals(t, false, nthConfig.IgnoreDaemonSets)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", nthConfig.KubernetesServiceHost)
	h.Equals(t, "KUBERNETES_SERVICE_PORT", nthConfig.KubernetesServicePort)
	h.Equals(t, "NODE_NAME", nthConfig.NodeName)
	h.Equals(t, 12345, nthConfig.NodeTerminationGracePeriod)
	h.Equals(t, "INSTANCE_METADATA_URL", nthConfig.MetadataURL)
	h.Equals(t, 12345, nthConfig.PodTerminationGracePeriod)
	h.Equals(t, "WEBHOOK_URL", nthConfig.WebhookURL)
	h.Equals(t, "WEBHOOK_HEADERS", nthConfig.WebhookHeaders)
	h.Equals(t, "WEBHOOK_TEMPLATE", nthConfig.WebhookTemplate)
	h.Equals(t, 100, nthConfig.MetadataTries)
	h.Equals(t, false, nthConfig.CordonOnly)
	h.Equals(t, true, nthConfig.UseAPIServerCacheToListPods)
	h.Equals(t, 30, nthConfig.HeartbeatInterval)
	h.Equals(t, 60, nthConfig.HeartbeatUntil)

	// Check that env vars were set
	value, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", value)

	value, ok = os.LookupEnv("KUBERNETES_SERVICE_PORT")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_PORT", value)
}

func TestParseCliArgsSuccess(t *testing.T) {
	resetFlagsForTest()
	os.Args = []string{
		"cmd",
		"--use-provider-id=true",
		"--delete-local-data=false",
		"--dry-run=true",
		"--enable-scheduled-event-draining=true",
		"--enable-spot-interruption-draining=false",
		"--enable-asg-lifecycle-draining=false",
		"--enable-sqs-termination-draining=true",
		"--enable-rebalance-monitoring=true",
		"--enable-rebalance-draining=true",
		"--ignore-daemon-sets=false",
		"--kubernetes-service-host=KUBERNETES_SERVICE_HOST",
		"--kubernetes-service-port=KUBERNETES_SERVICE_PORT",
		"--node-name=NODE_NAME",
		"--node-termination-grace-period=12345",
		"--metadata-url=INSTANCE_METADATA_URL",
		"--pod-termination-grace-period=12345",
		"--webhook-url=WEBHOOK_URL",
		"--webhook-headers=WEBHOOK_HEADERS",
		"--webhook-template=WEBHOOK_TEMPLATE",
		"--metadata-tries=100",
		"--cordon-only=false",
		"--use-apiserver-cache=true",
		"--heartbeat-interval=30",
		"--heartbeat-until=60",
	}
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, true, nthConfig.UseProviderId)
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
	h.Equals(t, false, nthConfig.EnableASGLifecycleDraining)
	h.Equals(t, true, nthConfig.EnableSQSTerminationDraining)
	h.Equals(t, true, nthConfig.EnableRebalanceMonitoring)
	h.Equals(t, true, nthConfig.EnableRebalanceDraining)
	h.Equals(t, false, nthConfig.IgnoreDaemonSets)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", nthConfig.KubernetesServiceHost)
	h.Equals(t, "KUBERNETES_SERVICE_PORT", nthConfig.KubernetesServicePort)
	h.Equals(t, "NODE_NAME", nthConfig.NodeName)
	h.Equals(t, 12345, nthConfig.NodeTerminationGracePeriod)
	h.Equals(t, "INSTANCE_METADATA_URL", nthConfig.MetadataURL)
	h.Equals(t, 12345, nthConfig.PodTerminationGracePeriod)
	h.Equals(t, "WEBHOOK_URL", nthConfig.WebhookURL)
	h.Equals(t, "WEBHOOK_HEADERS", nthConfig.WebhookHeaders)
	h.Equals(t, "WEBHOOK_TEMPLATE", nthConfig.WebhookTemplate)
	h.Equals(t, 100, nthConfig.MetadataTries)
	h.Equals(t, false, nthConfig.CordonOnly)
	h.Equals(t, false, nthConfig.EnablePrometheus)
	h.Equals(t, true, nthConfig.UseAPIServerCacheToListPods)
	h.Equals(t, 30, nthConfig.HeartbeatInterval)
	h.Equals(t, 60, nthConfig.HeartbeatUntil)

	// Check that env vars were set
	value, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", value)
}

func TestParseCliArgsOverrides(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("USE_PROVIDER_ID", "true")
	t.Setenv("DELETE_LOCAL_DATA", "true")
	t.Setenv("DRY_RUN", "false")
	t.Setenv("ENABLE_SCHEDULED_EVENT_DRAINING", "false")
	t.Setenv("ENABLE_SPOT_INTERRUPTION_DRAINING", "true")
	t.Setenv("ENABLE_ASG_LIFECYCLE_DRAINING", "true")
	t.Setenv("ENABLE_SQS_TERMINATION_DRAINING", "false")
	t.Setenv("ENABLE_REBALANCE_MONITORING", "true")
	t.Setenv("ENABLE_REBALANCE_DRAINING", "true")
	t.Setenv("GRACE_PERIOD", "99999")
	t.Setenv("IGNORE_DAEMON_SETS", "true")
	t.Setenv("KUBERNETES_SERVICE_HOST", "no")
	t.Setenv("KUBERNETES_SERVICE_PORT", "no")
	t.Setenv("NODE_NAME", "no")
	t.Setenv("NODE_TERMINATION_GRACE_PERIOD", "99999")
	t.Setenv("INSTANCE_METADATA_URL", "no")
	t.Setenv("POD_TERMINATION_GRACE_PERIOD", "99999")
	t.Setenv("WEBHOOK_URL", "no")
	t.Setenv("WEBHOOK_HEADERS", "no")
	t.Setenv("WEBHOOK_TEMPLATE", "no")
	t.Setenv("METADATA_TRIES", "100")
	t.Setenv("CORDON_ONLY", "true")
	t.Setenv("HEARTBEAT_INTERVAL", "3601")
	t.Setenv("HEARTBEAT_UNTIL", "172801")

	os.Args = []string{
		"cmd",
		"--use-provider-id=false",
		"--delete-local-data=false",
		"--dry-run=true",
		"--enable-scheduled-event-draining=true",
		"--enable-spot-interruption-draining=false",
		"--enable-asg-lifecycle-draining=false",
		"--enable-sqs-termination-draining=true",
		"--enable-rebalance-monitoring=false",
		"--enable-rebalance-draining=false",
		"--ignore-daemon-sets=false",
		"--kubernetes-service-host=KUBERNETES_SERVICE_HOST",
		"--kubernetes-service-port=KUBERNETES_SERVICE_PORT",
		"--node-name=NODE_NAME",
		"--node-termination-grace-period=12345",
		"--metadata-url=INSTANCE_METADATA_URL",
		"--pod-termination-grace-period=12345",
		"--webhook-url=WEBHOOK_URL",
		"--webhook-headers=WEBHOOK_HEADERS",
		"--webhook-template=WEBHOOK_TEMPLATE",
		"--metadata-tries=101",
		"--cordon-only=false",
		"--enable-prometheus-server=true",
		"--prometheus-server-port=2112",
		"--heartbeat-interval=3600",
		"--heartbeat-until=172800",
	}
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, false, nthConfig.UseProviderId)
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
	h.Equals(t, false, nthConfig.EnableASGLifecycleDraining)
	h.Equals(t, true, nthConfig.EnableSQSTerminationDraining)
	h.Equals(t, false, nthConfig.EnableRebalanceMonitoring)
	h.Equals(t, false, nthConfig.EnableRebalanceDraining)
	h.Equals(t, false, nthConfig.IgnoreDaemonSets)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", nthConfig.KubernetesServiceHost)
	h.Equals(t, "KUBERNETES_SERVICE_PORT", nthConfig.KubernetesServicePort)
	h.Equals(t, "NODE_NAME", nthConfig.NodeName)
	h.Equals(t, 12345, nthConfig.NodeTerminationGracePeriod)
	h.Equals(t, "INSTANCE_METADATA_URL", nthConfig.MetadataURL)
	h.Equals(t, 12345, nthConfig.PodTerminationGracePeriod)
	h.Equals(t, "WEBHOOK_URL", nthConfig.WebhookURL)
	h.Equals(t, "WEBHOOK_HEADERS", nthConfig.WebhookHeaders)
	h.Equals(t, "WEBHOOK_TEMPLATE", nthConfig.WebhookTemplate)
	h.Equals(t, 101, nthConfig.MetadataTries)
	h.Equals(t, false, nthConfig.CordonOnly)
	h.Equals(t, true, nthConfig.EnablePrometheus)
	h.Equals(t, 2112, nthConfig.PrometheusPort)
	h.Equals(t, 3600, nthConfig.HeartbeatInterval)
	h.Equals(t, 172800, nthConfig.HeartbeatUntil)

	// Check that env vars were set
	value, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", value)
}

func TestParseCliArgsWithGracePeriodSuccess(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("POD_TERMINATION_GRACE_PERIOD", "")
	t.Setenv("NODE_NAME", "bla")
	t.Setenv("GRACE_PERIOD", "12")

	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)
	h.Equals(t, 12, nthConfig.PodTerminationGracePeriod)
}

func TestParseCliArgsMissingNodeNameFailure(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("NODE_NAME", "")
	_, err := config.ParseCliArgs()
	h.Assert(t, err != nil, "Failed to return error when node-name not provided")
}

func TestParseCliArgsCreateFlagsFailure(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("DELETE_LOCAL_DATA", "something not true or false")
	_, err := config.ParseCliArgs()
	h.Assert(t, err != nil, "Failed to return error when creating flags")
}

func TestParseCliArgsAWSSession(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("ENABLE_SQS_TERMINATION_DRAINING", "true")
	t.Setenv("AWS_REGION", "us-weast-1")
	t.Setenv("NODE_NAME", "node")
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)
	h.Assert(t, nthConfig.AWSRegion == "us-weast-1", "Should find region as us-weast-1")
}

func TestPrint_Human(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("NODE_NAME", "node")
	t.Setenv("JSON_LOGGING", "false")
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)
	var printBuf bytes.Buffer
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: &printBuf})
	nthConfig.Print()
	var humanBuf bytes.Buffer
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: &humanBuf})
	nthConfig.PrintHumanConfigArgs()
	h.Assert(t, humanBuf.String() == printBuf.String(), "Should have printed non-JSON formatted config values")
}

func TestPrint_JSON(t *testing.T) {
	resetFlagsForTest()
	t.Setenv("NODE_NAME", "node")
	t.Setenv("JSON_LOGGING", "true")
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)
	var printBuf bytes.Buffer
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: &printBuf})
	nthConfig.Print()
	var jsonBuf bytes.Buffer
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: &jsonBuf})
	nthConfig.PrintJsonConfigArgs()
	h.Assert(t, jsonBuf.String() == printBuf.String(), "Should have printed JSON formatted config values")
}
