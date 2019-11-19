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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
)

const (
	nodeInterruptionDuration = 2 * time.Minute
	// EC2 Instance Metadata is configurable mainly for testing purposes
	instanceMetadataUrlConfigKey       = "INSTANCE_METADATA_URL"
	dryRunConfigKey                    = "DRY_RUN"
	nodeNameConfigKey                  = "NODE_NAME"
	kubernetesServiceHostConfigKey     = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePortConfigKey     = "KUBERNETES_SERVICE_PORT"
	deleteLocalDataConfigKey           = "DELETE_LOCAL_DATA"
	ignoreDaemonSetsConfigKey          = "IGNORE_DAEMON_SETS"
	podTerminationGracePeriodConfigKey = "GRACE_PERIOD"
	defaultInstanceMetadataUrl         = "http://169.254.169.254"
)

// arguments set via CLI, environment variables, or defaults
var dryRun bool
var nodeName string
var metadataUrl string
var ignoreDaemonSets bool
var deleteLocalData bool
var kubernetesServiceHost string
var kubernetesServicePort string
var podTerminationGracePeriod int

// InstanceActionDetail metadata structure for json parsing
type InstanceActionDetail struct {
	InstanceId     string `json:"instance-id"`
	InstanceAction string `json:"instance-action"`
}

// InstanceAction metadata structure for json parsing
type InstanceAction struct {
	Version    string               `json:"version"`
	Id         string               `json:"id"`
	DetailType string               `json:"detail-type"`
	Source     string               `json:"source"`
	Account    string               `json:"account"`
	Time       string               `json:"time"`
	Region     string               `json:"region"`
	Resources  []string             `json:"resources"`
	Detail     InstanceActionDetail `json:"detail"`
}

func requestMetadata() (*http.Response, error) {
	return http.Get(metadataUrl + "/latest/meta-data/spot/instance-action")
}

func retry(attempts int, sleep time.Duration) (*http.Response, error) {
	log.Printf("Request to instance metadata failed. Retrying.\n")
	resp, err := requestMetadata()
	if err != nil {
		if attempts--; attempts > 0 {
			jitter := time.Duration(rand.Int63n(int64(sleep)))
			sleep = sleep + jitter/2

			log.Printf("Retry failed. Attempts remaining: %d\n", attempts)
			log.Printf("Sleep for %s seconds\n", sleep)
			time.Sleep(sleep)
			return retry(attempts, 2*sleep)
		}

		log.Fatalln("Error getting response from instance metadata ", err.Error())
	}

	return resp, err
}

func shouldDrainNode() bool {
	resp, err := requestMetadata()
	if err != nil {
		resp, err = retry(3, 2*time.Second)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	var instanceAction InstanceAction
	json.NewDecoder(resp.Body).Decode(&instanceAction)
	interruptionTime, err := time.Parse(time.RFC3339, instanceAction.Time)
	if err != nil {
		log.Fatalln("Could not parse time from metadata json", err.Error())
	}
	timeUntilInterruption := time.Now().Sub(interruptionTime)
	if timeUntilInterruption <= nodeInterruptionDuration {
		return true
	}
	return false
}

func waitForTermination() {
	for range time.Tick(time.Second * 5) {
		if shouldDrainNode() {
			break
		}
	}
}

func getDrainHelper(nodeName string) *drain.Helper {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalln("Failed to create in-cluster config: ", err.Error())
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalln("Failed to create kubernetes clientset: ", err.Error())
	}

	return &drain.Helper{
		Client:              clientset,
		Force:               true,
		GracePeriodSeconds:  podTerminationGracePeriod,
		IgnoreAllDaemonSets: ignoreDaemonSets,
		DeleteLocalData:     deleteLocalData,
		Timeout:             nodeInterruptionDuration,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
	}
}

// Get env var or default
func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
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

func parseCliArgs() {
	flag.BoolVar(&dryRun, "dry-run", getBoolEnv(dryRunConfigKey, false), "If true, only log if a node would be drained")
	flag.StringVar(&nodeName, "node-name", getEnv(nodeNameConfigKey, ""), "The kubernetes node name")
	flag.StringVar(&metadataUrl, "metadata-url", getEnv(instanceMetadataUrlConfigKey, defaultInstanceMetadataUrl), "The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing.")
	flag.BoolVar(&ignoreDaemonSets, "ignore-daemon-sets", getBoolEnv(ignoreDaemonSetsConfigKey, true), "If true, drain daemon sets when a spot interrupt is received.")
	flag.BoolVar(&deleteLocalData, "delete-local-data", getBoolEnv(deleteLocalDataConfigKey, true), "If true, do not drain pods that are using local node storage in emptyDir")
	flag.StringVar(&kubernetesServiceHost, "kubernetes-service-host", getEnv(kubernetesServiceHostConfigKey, ""), "[ADVANCED] The k8s service host to send api calls to.")
	flag.StringVar(&kubernetesServicePort, "kubernetes-service-port", getEnv(kubernetesServicePortConfigKey, ""), "[ADVANCED] The k8s service port to send api calls to.")
	flag.IntVar(&podTerminationGracePeriod, "grace-period", getIntEnv(podTerminationGracePeriodConfigKey, -1), "Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.")

	flag.Parse()

	if nodeName == "" {
		log.Fatalln("You must provide a node-name to the CLI or NODE_NAME environment variable.")
	}
	// client-go expects these to be set in env vars
	os.Setenv(kubernetesServiceHostConfigKey, kubernetesServiceHost)
	os.Setenv(kubernetesServicePortConfigKey, kubernetesServicePort)

	fmt.Printf("aws-node-termination-handler arguments: \n"+
		"\tdry-run: %t,\n"+
		"\tnode-name: %s,\n"+
		"\tmetadata-url: %s,\n"+
		"\tkubernetes-service-host: %s,\n"+
		"\tkubernetes-service-port: %s,\n"+
		"\tdelete-local-data: %t,\n"+
		"\tignore-daemon-sets: %t\n"+
		"\tgrace-period: %d\n",
		dryRun, nodeName, metadataUrl, kubernetesServiceHost, kubernetesServicePort, deleteLocalData, ignoreDaemonSets, podTerminationGracePeriod)
}

func main() {
	var dryRunMessageSuffix = "but dry-run flag was set"
	parseCliArgs()
	helper := getDrainHelper(nodeName)

	node, err := helper.Client.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Couldn't get node %q: %s\n", nodeName, err.Error())
	}

	log.Println("Kubernetes Spot Node Termination Handler has started successfully!")
	waitForTermination()

	if dryRun {
		log.Printf("Node %s would have been cordoned, %s", nodeName, dryRunMessageSuffix)
	} else {
		err = drain.RunCordonOrUncordon(helper, node, true)
		if err != nil {
			log.Fatalf("Couldn't cordon node %q: %s\n", nodeName, err.Error())
		}
	}

	if dryRun {
		log.Printf("Node %s would have been drained, %s", nodeName, dryRunMessageSuffix)
	} else {
		// Delete all pods on the node
		err = drain.RunNodeDrain(helper, nodeName)
		if err != nil {
			log.Fatalln(err.Error())
		}
	}

	log.Printf("Node %q successfully drained.\n", nodeName)

	// Sleep to prevent process from restarting.
	// The node should be terminated by 2 minutes.
	time.Sleep(nodeInterruptionDuration)
}
