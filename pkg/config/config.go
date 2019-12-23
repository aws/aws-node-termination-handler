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
	"log"
	"os"
	"strconv"
)

const (
	// EC2 Instance Metadata is configurable mainly for testing purposes
	instanceMetadataURLConfigKey        = "INSTANCE_METADATA_URL"
	defaultInstanceMetadataURL          = "http://169.254.169.254"
	dryRunConfigKey                     = "DRY_RUN"
	nodeNameConfigKey                   = "NODE_NAME"
	kubernetesServiceHostConfigKey      = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePortConfigKey      = "KUBERNETES_SERVICE_PORT"
	deleteLocalDataConfigKey            = "DELETE_LOCAL_DATA"
	ignoreDaemonSetsConfigKey           = "IGNORE_DAEMON_SETS"
	gracePeriodConfigKey                = "GRACE_PERIOD"
	podTerminationGracePeriodConfigKey  = "POD_TERMINATION_GRACE_PERIOD"
	podTerminationGracePeriodDefault    = -1
	nodeTerminationGracePeriodConfigKey = "NODE_TERMINATION_GRACE_PERIOD"
	nodeTerminationGracePeriodDefault   = 120
)

//Config arguments set via CLI, environment variables, or defaults
type Config struct {
	DryRun                     bool
	NodeName                   string
	MetadataURL                string
	IgnoreDaemonSets           bool
	DeleteLocalData            bool
	KubernetesServiceHost      string
	KubernetesServicePort      string
	PodTerminationGracePeriod  int
	NodeTerminationGracePeriod int
}

//ParseCliArgs parses cli arguments and uses environment variables as fallback values
func ParseCliArgs() Config {
	config := Config{}
	var gracePeriod int
	flag.BoolVar(&config.DryRun, "dry-run", getBoolEnv(dryRunConfigKey, false), "If true, only log if a node would be drained")
	flag.StringVar(&config.NodeName, "node-name", getEnv(nodeNameConfigKey, ""), "The kubernetes node name")
	flag.StringVar(&config.MetadataURL, "metadata-url", getEnv(instanceMetadataURLConfigKey, defaultInstanceMetadataURL), "The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing.")
	flag.BoolVar(&config.IgnoreDaemonSets, "ignore-daemon-sets", getBoolEnv(ignoreDaemonSetsConfigKey, true), "If true, drain daemon sets when a spot interrupt is received.")
	flag.BoolVar(&config.DeleteLocalData, "delete-local-data", getBoolEnv(deleteLocalDataConfigKey, true), "If true, do not drain pods that are using local node storage in emptyDir")
	flag.StringVar(&config.KubernetesServiceHost, "kubernetes-service-host", getEnv(kubernetesServiceHostConfigKey, ""), "[ADVANCED] The k8s service host to send api calls to.")
	flag.StringVar(&config.KubernetesServicePort, "kubernetes-service-port", getEnv(kubernetesServicePortConfigKey, ""), "[ADVANCED] The k8s service port to send api calls to.")
	flag.IntVar(&gracePeriod, "grace-period", getIntEnv(gracePeriodConfigKey, podTerminationGracePeriodDefault), "[DEPRECATED] * Use pod-termination-grace-period instead * Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.")
	flag.IntVar(&config.PodTerminationGracePeriod, "pod-termination-grace-period", getIntEnv(podTerminationGracePeriodConfigKey, podTerminationGracePeriodDefault), "Period of time in seconds given to each POD to terminate gracefully. If negative, the default value specified in the pod will be used.")
	flag.IntVar(&config.NodeTerminationGracePeriod, "node-termination-grace-period", getIntEnv(nodeTerminationGracePeriodConfigKey, nodeTerminationGracePeriodDefault), "Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled based on this value to optimize the amount of compute time, but still safely drain the node before an event.")

	flag.Parse()

	if isConfigProvided("pod-termination-grace-period", podTerminationGracePeriodConfigKey) && isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Println("Deprecated argument \"grace-period\" and the replacement argument \"pod-termination-grace-period\" was provided. Using the newer argument \"pod-termination-grace-period\"")
	} else if isConfigProvided("grace-period", gracePeriodConfigKey) {
		log.Println("Deprecated argument \"grace-period\" was provided. This argument will eventually be removed. Please switch to \"pod-termination-grace-period\" instead.")
		config.PodTerminationGracePeriod = gracePeriod
	}

	if config.NodeName == "" {
		log.Fatalln("You must provide a node-name to the CLI or NODE_NAME environment variable.")
	}
	// client-go expects these to be set in env vars
	os.Setenv(kubernetesServiceHostConfigKey, config.KubernetesServiceHost)
	os.Setenv(kubernetesServicePortConfigKey, config.KubernetesServicePort)

	fmt.Printf("aws-node-termination-handler arguments: \n"+
		"\tdry-run: %t,\n"+
		"\tnode-name: %s,\n"+
		"\tmetadata-url: %s,\n"+
		"\tkubernetes-service-host: %s,\n"+
		"\tkubernetes-service-port: %s,\n"+
		"\tdelete-local-data: %t,\n"+
		"\tignore-daemon-sets: %t\n"+
		"\tpod-termination-grace-period: %d\n"+
		"\tnode-termination-grace-period: %d\n",
		config.DryRun,
		config.NodeName,
		config.MetadataURL,
		config.KubernetesServiceHost,
		config.KubernetesServicePort,
		config.DeleteLocalData,
		config.IgnoreDaemonSets,
		config.PodTerminationGracePeriod,
		config.NodeTerminationGracePeriod)

	return config
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
		log.Fatalln("Env Var " + key + " must be an integer")
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
		log.Fatalln("Env Var " + key + " must be either true or false")
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
