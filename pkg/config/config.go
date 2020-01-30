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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
)

const (
	// EC2 Instance Metadata is configurable mainly for testing purposes
	defaultInstanceMetadataURL              = "http://169.254.169.254"
	deleteLocalDataConfigKey                = "DELETE_LOCAL_DATA"
	dryRunConfigKey                         = "DRY_RUN"
	enableScheduledEventDrainingConfigKey   = "ENABLE_SCHEDULED_EVENT_DRAINING"
	enableScheduledEventDrainingDefault     = false
	enableSpotInterruptionDrainingConfigKey = "ENABLE_SPOT_INTERRUPTION_DRAINING"
	enableSpotInterruptionDrainingDefault   = true
	gracePeriodConfigKey                    = "GRACE_PERIOD"
	ignoreDaemonSetsConfigKey               = "IGNORE_DAEMON_SETS"
	instanceMetadataURLConfigKey            = "INSTANCE_METADATA_URL"
	kubernetesServiceHostConfigKey          = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePortConfigKey          = "KUBERNETES_SERVICE_PORT"
	nodeNameConfigKey                       = "NODE_NAME"
	nodeTerminationGracePeriodConfigKey     = "NODE_TERMINATION_GRACE_PERIOD"
	nodeTerminationGracePeriodDefault       = 120
	podTerminationGracePeriodConfigKey      = "POD_TERMINATION_GRACE_PERIOD"
	podTerminationGracePeriodDefault        = -1
	webhookURLConfigKey                     = "WEBHOOK_URL"
	webhookURLDefault                       = ""
	webhookHeadersConfigKey                 = "WEBHOOK_HEADERS"
	webhookHeadersDefault                   = `{"Content-type":"application/json"}`
	webhookTemplateConfigKey                = "WEBHOOK_TEMPLATE"
	webhookTemplateDefault                  = `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Description: {{ .Description }} - State: {{ .State }} - Start Time: {{ .StartTime }}"}`
)

//Config arguments set via CLI, environment variables, or defaults
type Config struct {
	DeleteLocalData                bool   `json:"delete-local-data"`
	DryRun                         bool   `json:"dry-run"`
	EnableScheduledEventDraining   bool   `json:"enable-scheduled-event-draining"`
	EnableSpotInterruptionDraining bool   `json:"enable-spot-interruption-draining"`
	IgnoreDaemonSets               bool   `json:"ignore-daemon-sets"`
	KubernetesServiceHost          string `json:"kubernetes-service-host"`
	KubernetesServicePort          string `json:"kubernetes-service-port"`
	MetadataURL                    string `json:"metadata-url"`
	NodeName                       string `json:"node-name"`
	NodeTerminationGracePeriod     int    `json:"node-termination-grace-period"`
	PodTerminationGracePeriod      int    `json:"pod-termination-grace-period"`
	WebhookURL                     string `json:"webhook-url"`
	WebhookHeaders                 string `json:"webhook-headers"`
	WebhookTemplate                string `json:"webhook-template"`
}

type flagData struct {
	Key      string
	DefValue interface{}
	Usage    string
}

var flags = map[string]flagData{
	"delete-local-data": flagData{
		Key:      deleteLocalDataConfigKey,
		DefValue: true,
		Usage:    "If true, do not drain pods that are using local node storage in emptyDir",
	},
	"dry-run": flagData{
		Key:      dryRunConfigKey,
		DefValue: false,
		Usage:    "If true, only log if a node would be drained",
	},
	"enable-scheduled-event-draining": flagData{
		Key:      enableScheduledEventDrainingConfigKey,
		DefValue: enableScheduledEventDrainingDefault,
		Usage:    "[EXPERIMENTAL] If true, drain nodes before the maintenance window starts for an EC2 instance scheduled event",
	},
	"enable-spot-interruption-draining": flagData{
		Key:      enableSpotInterruptionDrainingConfigKey,
		DefValue: enableSpotInterruptionDrainingDefault,
		Usage:    "If true, drain nodes when the spot interruption termination notice is receieved",
	},
	"grace-period": flagData{
		Key:      gracePeriodConfigKey,
		DefValue: podTerminationGracePeriodDefault,
		Usage: "[DEPRECATED] * Use pod-termination-grace-period instead * Period of time in seconds given to each " +
			"pod to terminate gracefully. If negative, the default value specified in the pod will be used.",
	},
	"ignore-daemon-sets": flagData{
		Key:      ignoreDaemonSetsConfigKey,
		DefValue: true,
		Usage:    "If true, drain daemon sets when a spot interrupt is received.",
	},
	"kubernetes-service-host": flagData{
		Key:      kubernetesServiceHostConfigKey,
		DefValue: "",
		Usage:    "[ADVANCED] The k8s service host to send api calls to.",
	},
	"kubernetes-service-port": flagData{
		Key:      kubernetesServicePortConfigKey,
		DefValue: "",
		Usage:    "[ADVANCED] The k8s service port to send api calls to.",
	},
	"node-name": flagData{
		Key:      nodeNameConfigKey,
		DefValue: "",
		Usage:    "The kubernetes node name",
	},
	"node-termination-grace-period": flagData{
		Key:      nodeTerminationGracePeriodConfigKey,
		DefValue: nodeTerminationGracePeriodDefault,
		Usage: "Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled " +
			"based on this value to optimize the amount of compute time, but still safely drain the node before an event.",
	},
	"metadata-url": flagData{
		Key:      instanceMetadataURLConfigKey,
		DefValue: defaultInstanceMetadataURL,
		Usage:    "If true, only log if a node would be drained",
	},
	"pod-termination-grace-period": flagData{
		Key:      podTerminationGracePeriodConfigKey,
		DefValue: podTerminationGracePeriodDefault,
		Usage: "Period of time in seconds given to each POD to terminate gracefully. If negative, the default " +
			"value specified in the pod will be used.",
	},
	"webhook-url": flagData{
		Key:      webhookURLConfigKey,
		DefValue: webhookURLDefault,
		Usage:    "If specified, posts event data to URL upon instance interruption action.",
	},
	"webhook-headers": flagData{
		Key:      webhookHeadersConfigKey,
		DefValue: webhookHeadersDefault,
		Usage:    "If specified, replaces the default webhook headers.",
	},
	"webhook-template": flagData{
		Key:      webhookTemplateConfigKey,
		DefValue: webhookTemplateDefault,
		Usage:    "If specified, replaces the default webhook message template.",
	},
}

//ParseCliArgs parses cli arguments and uses environment variables as fallback values
func ParseCliArgs() (Config, error) {
	config := Config{}

	results, err := createFlags(flags)
	if err != nil {
		return config, err
	}
	gracePeriod := results["grace-period"].(int)

	// Converts flag results into []byte
	bytes, err := json.Marshal(results)
	if err != nil {
		return config, err
	}

	// Generate the config struct from []byte
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return config, err
	}

	if isConfigProvided("pod-termination-grace-period", podTerminationGracePeriodConfigKey) && isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Println("Deprecated argument \"grace-period\" and the replacement argument \"pod-termination-grace-period\" was provided. Using the newer argument \"pod-termination-grace-period\"")
	} else if isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Println("Deprecated argument \"grace-period\" was provided. This argument will eventually be removed. Please switch to \"pod-termination-grace-period\" instead.")
		config.PodTerminationGracePeriod = gracePeriod
	}

	if config.NodeName == "" {
		return config, errors.New("must provide a node-name to the CLI or NODE_NAME environment variable")
	}

	// client-go expects these to be set in env vars
	os.Setenv(kubernetesServiceHostConfigKey, config.KubernetesServiceHost)
	os.Setenv(kubernetesServicePortConfigKey, config.KubernetesServicePort)

	// intentionally did not log webhook configuration as there may be secrets
	fmt.Printf(
		"aws-node-termination-handler arguments: \n"+
			"\tdry-run: %t,\n"+
			"\tnode-name: %s,\n"+
			"\tmetadata-url: %s,\n"+
			"\tkubernetes-service-host: %s,\n"+
			"\tkubernetes-service-port: %s,\n"+
			"\tdelete-local-data: %t,\n"+
			"\tignore-daemon-sets: %t,\n"+
			"\tpod-termination-grace-period: %d,\n"+
			"\tnode-termination-grace-period: %d,\n"+
			"\tenable-scheduled-event-draining: %t,\n"+
			"\tenable-spot-interruption-draining: %t,\n",
		config.DryRun,
		config.NodeName,
		config.MetadataURL,
		config.KubernetesServiceHost,
		config.KubernetesServicePort,
		config.DeleteLocalData,
		config.IgnoreDaemonSets,
		config.PodTerminationGracePeriod,
		config.NodeTerminationGracePeriod,
		config.EnableScheduledEventDraining,
		config.EnableSpotInterruptionDraining,
	)

	return config, nil
}

func createFlags(flagData map[string]flagData) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for name, data := range flagData {
		switch data.DefValue.(type) {
		case string:
			value := getEnv(data.Key, data.DefValue.(string))
			var flagValue string
			flag.StringVar(&flagValue, name, value, data.Usage)
			result[name] = flagValue
		case int:
			value, err := getIntEnv(data.Key, data.DefValue.(int))
			if err != nil {
				return result, err
			}
			var flagValue int
			flag.IntVar(&flagValue, name, value, data.Usage)
			result[name] = flagValue
		case bool:
			value, err := getBoolEnv(data.Key, data.DefValue.(bool))
			if err != nil {
				return result, err
			}
			var flagValue bool
			flag.BoolVar(&flagValue, name, value, data.Usage)
			result[name] = flagValue
		default:
			return result, errors.New("Unrecognized defValue type for " + name)
		}
	}
	flag.Parse()
	return result, nil
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
func getIntEnv(key string, fallback int) (int, error) {
	envStrValue := getEnv(key, "")
	if envStrValue == "" {
		return fallback, nil
	}
	envIntValue, err := strconv.Atoi(envStrValue)
	if err != nil {
		return -1, err
	}
	return envIntValue, nil
}

// Parse env var to boolean if key exists
func getBoolEnv(key string, fallback bool) (bool, error) {
	envStrValue := getEnv(key, "")
	if envStrValue == "" {
		return fallback, nil
	}
	envBoolValue, err := strconv.ParseBool(envStrValue)
	if err != nil {
		return false, errors.New("Env Var " + key + " must be either true or false")
	}
	return envBoolValue, nil
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
