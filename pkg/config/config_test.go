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
	"flag"
	"os"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func resetFlagsForTest() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"cmd"}
}

func TestParseCliArgsEnvSuccess(t *testing.T) {
	resetFlagsForTest()
	os.Setenv("DELETE_LOCAL_DATA", "false")
	os.Setenv("DRY_RUN", "true")
	os.Setenv("ENABLE_SCHEDULED_EVENT_DRAINING", "true")
	os.Setenv("ENABLE_SPOT_INTERRUPTION_DRAINING", "false")
	os.Setenv("GRACE_PERIOD", "12345")
	os.Setenv("IGNORE_DAEMON_SETS", "false")
	os.Setenv("KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_HOST")
	os.Setenv("KUBERNETES_SERVICE_PORT", "KUBERNETES_SERVICE_PORT")
	os.Setenv("NODE_NAME", "NODE_NAME")
	os.Setenv("NODE_TERMINATION_GRACE_PERIOD", "12345")
	os.Setenv("INSTANCE_METADATA_URL", "INSTANCE_METADATA_URL")
	os.Setenv("POD_TERMINATION_GRACE_PERIOD", "12345")
	os.Setenv("WEBHOOK_URL", "WEBHOOK_URL")
	os.Setenv("WEBHOOK_HEADERS", "WEBHOOK_HEADERS")
	os.Setenv("WEBHOOK_TEMPLATE", "WEBHOOK_TEMPLATE")
	os.Setenv("METADATA_TRIES", "100")
	os.Setenv("CORDON_ONLY", "false")
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
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
		"--delete-local-data=false",
		"--dry-run=true",
		"--enable-scheduled-event-draining=true",
		"--enable-spot-interruption-draining=false",
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
	}
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
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

	// Check that env vars were set
	value, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", value)
}

func TestParseCliArgsOverrides(t *testing.T) {
	resetFlagsForTest()
	os.Setenv("DELETE_LOCAL_DATA", "true")
	os.Setenv("DRY_RUN", "false")
	os.Setenv("ENABLE_SCHEDULED_EVENT_DRAINING", "false")
	os.Setenv("ENABLE_SPOT_INTERRUPTION_DRAINING", "true")
	os.Setenv("GRACE_PERIOD", "99999")
	os.Setenv("IGNORE_DAEMON_SETS", "true")
	os.Setenv("KUBERNETES_SERVICE_HOST", "no")
	os.Setenv("KUBERNETES_SERVICE_PORT", "no")
	os.Setenv("NODE_NAME", "no")
	os.Setenv("NODE_TERMINATION_GRACE_PERIOD", "99999")
	os.Setenv("INSTANCE_METADATA_URL", "no")
	os.Setenv("POD_TERMINATION_GRACE_PERIOD", "99999")
	os.Setenv("WEBHOOK_URL", "no")
	os.Setenv("WEBHOOK_HEADERS", "no")
	os.Setenv("WEBHOOK_TEMPLATE", "no")
	os.Setenv("METADATA_TRIES", "100")
	os.Setenv("CORDON_ONLY", "true")
	os.Args = []string{
		"cmd",
		"--delete-local-data=false",
		"--dry-run=true",
		"--enable-scheduled-event-draining=true",
		"--enable-spot-interruption-draining=false",
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
	}
	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)

	// Assert all the values were set
	h.Equals(t, false, nthConfig.DeleteLocalData)
	h.Equals(t, true, nthConfig.DryRun)
	h.Equals(t, true, nthConfig.EnableScheduledEventDraining)
	h.Equals(t, false, nthConfig.EnableSpotInterruptionDraining)
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

	// Check that env vars were set
	value, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	h.Equals(t, true, ok)
	h.Equals(t, "KUBERNETES_SERVICE_HOST", value)
}

func TestParseCliArgsWithGracePeriodSuccess(t *testing.T) {
	resetFlagsForTest()
	os.Setenv("POD_TERMINATION_GRACE_PERIOD", "")
	os.Setenv("NODE_NAME", "bla")
	os.Setenv("GRACE_PERIOD", "12")

	nthConfig, err := config.ParseCliArgs()
	h.Ok(t, err)
	h.Equals(t, 12, nthConfig.PodTerminationGracePeriod)
}

func TestParseCliArgsMissingNodeNameFailure(t *testing.T) {
	resetFlagsForTest()
	os.Setenv("NODE_NAME", "")
	_, err := config.ParseCliArgs()
	h.Assert(t, err != nil, "Failed to return error when node-name not provided")
}

func TestParseCliArgsCreateFlagsFailure(t *testing.T) {
	resetFlagsForTest()
	os.Setenv("DELETE_LOCAL_DATA", "something not true or false")
	_, err := config.ParseCliArgs()
	h.Assert(t, err != nil, "Failed to return error when creating flags")
}
