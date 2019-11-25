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
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
)

const (
	nodeInterruptionDuration = 2 * time.Minute
	// EC2 Instance Metadata is configurable mainly for testing purposes
	instanceMetadataUrlConfigKey = "INSTANCE_METADATA_URL"
	defaultInstanceMetadataUrl   = "http://169.254.169.254"
)

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
	metadataUrl := getEnv(instanceMetadataUrlConfigKey, defaultInstanceMetadataUrl)
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
		GracePeriodSeconds:  30, //default k8s value
		IgnoreAllDaemonSets: true,
		Timeout:             time.Second * 60,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
	}
}

// Get env var or default
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	nodeName := os.Getenv("NODE_NAME")
	if len(nodeName) == 0 {
		log.Fatalln("Failed to get NODE_NAME from environment. " +
			"Check that the kubernetes yaml file is configured correctly")
	}
	helper := getDrainHelper(nodeName)

	node, err := helper.Client.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Couldn't get node %q: %s\n", nodeName, err.Error())
	}

	log.Println("Kubernetes Spot Node Termination Handler has started successfully!")
	waitForTermination()

	err = drain.RunCordonOrUncordon(helper, node, true)
	if err != nil {
		log.Fatalf("Couldn't cordon node %q: %s\n", nodeName, err.Error())
	}

	// Delete all pods on the node
	err = drain.RunNodeDrain(helper, nodeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	log.Printf("Node %q successfully drained.\n", nodeName)

	// Sleep to prevent process from restarting.
	// The node should be terminated by 2 minutes.
	time.Sleep(nodeInterruptionDuration)
}
