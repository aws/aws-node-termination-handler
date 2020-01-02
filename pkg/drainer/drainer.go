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

package drainer

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
)

var drainHelper *drain.Helper
var node *corev1.Node
var nthConfig config.Config

// InitDrainer will ensure the drainer has all the resources necessary to complete a drain.
// This function must be called before any function in the drainer package.
func InitDrainer(inputNthConfig config.Config) {
	nthConfig = inputNthConfig
	if drainHelper == nil {
		drainHelper = getDrainHelper()
	}
	var err error
	node, err = FetchNode()
	if err != nil {
		log.Fatalf("Couldn't get node %q: %s\n", nthConfig.NodeName, err.Error())
	}
	log.Printf("Successufully retrieved node: %s", node.Name)
}

// FetchNode will send an http request to the k8s api server and return the corev1 model node
func FetchNode() (*corev1.Node, error) {
	node := &corev1.Node{}
	if nthConfig.DryRun {
		return node, nil
	}
	return drainHelper.Client.CoreV1().Nodes().Get(nthConfig.NodeName, metav1.GetOptions{})
}

// IsNodeUnschedulable checks if the node is marked as unschedulable
func IsNodeUnschedulable() (bool, error) {
	if nthConfig.DryRun {
		return false, nil
	}
	node, err := FetchNode()
	if err != nil {
		return true, err
	}
	return node.Spec.Unschedulable, nil
}

//Drain will cordon the node and evict pods based on the config
func Drain() {
	if drainHelper == nil || node == nil {
		InitDrainer(nthConfig)
	}
	var err error
	if nthConfig.DryRun {
		log.Printf("Node %s would have been cordoned and drained, but dry-run flag was set\n", nthConfig.NodeName)
		return
	}
	cordonNode()
	// Delete all pods on the node
	err = drain.RunNodeDrain(drainHelper, nthConfig.NodeName)
	if err != nil {
		log.Fatalln(err.Error())
	}
}

// Cordon will add a NoSchedule on the node
func cordonNode() {
	err := drain.RunCordonOrUncordon(drainHelper, node, true)
	if err != nil {
		log.Fatalf("Couldn't cordon node %q: %s\n", node, err.Error())
	}
}

// UncordonNode will remove the NoSchedule on the node
func UncordonNode() error {
	if nthConfig.DryRun {
		log.Printf("Node %s would have been uncordoned, but dry-run flag was set", nthConfig.NodeName)
		return nil
	}
	err := drain.RunCordonOrUncordon(drainHelper, node, false)
	if err != nil {
		log.Printf("Couldn't uncordon node %q: %s\n", node, err.Error())
		return err
	}
	return nil
}

// AddNodeLabel will add a label to the node given a label key and value
func AddNodeLabel(key string, value string) error {
	type metadata struct {
		Labels map[string]string `json:"labels"`
	}
	type patch struct {
		Metadata metadata `json:"metadata"`
	}
	labels := make(map[string]string)
	labels[key] = value
	payload := patch{
		Metadata: metadata{
			Labels: labels,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("An error occured while marshalling the json to add a label to the node %v", err)
		return err
	}
	if nthConfig.DryRun {
		log.Printf("Would have added label (%s=%s) to node %s, but dry-run flag was set", key, value, nthConfig.NodeName)
		return nil
	}
	_, err = drainHelper.Client.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, payloadBytes)
	if err != nil {
		log.Printf("%v node Patch failed when adding a label to the node %v", node.Name, err)
		return err
	}
	return nil
}

// RemoveNodeLabel will remove a node label given a label key
func RemoveNodeLabel(key string) error {
	type patchRequest struct {
		Op   string `json:"op"`
		Path string `json:"path"`
	}

	var patchReqs []interface{}
	patchRemove := patchRequest{
		Op:   "remove",
		Path: fmt.Sprintf("/metadata/labels/%s", jsonPatchEscape(key)),
	}
	payload, err := json.Marshal(append(patchReqs, patchRemove))
	if err != nil {
		log.Printf("An error occured while marshalling the json to remove a label from the node %v", err)
		return err
	}
	if nthConfig.DryRun {
		log.Printf("Would have removed label with key %s from node %s, but dry-run flag was set", key, nthConfig.NodeName)
		return nil
	}
	_, err = drainHelper.Client.CoreV1().Nodes().Patch(node.Name, types.JSONPatchType, payload)
	if err != nil {
		log.Printf("%v node Patch failed when removing a label from the node %v", node.Name, err)
		return err
	}
	return nil
}

func jsonPatchEscape(value string) string {
	value = strings.Replace(value, "~", "~0", -1)
	return strings.Replace(value, "/", "~1", -1)
}

func getDrainHelper() *drain.Helper {
	var clientset = &kubernetes.Clientset{}
	if !nthConfig.DryRun {
		config, err := rest.InClusterConfig()
		if err != nil {
			log.Fatalln("Failed to create in-cluster config: ", err.Error())
		}

		// creates the clientset
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			log.Fatalln("Failed to create kubernetes clientset: ", err.Error())
		}
	}

	return &drain.Helper{
		Client:              clientset,
		Force:               true,
		GracePeriodSeconds:  nthConfig.PodTerminationGracePeriod,
		IgnoreAllDaemonSets: nthConfig.IgnoreDaemonSets,
		DeleteLocalData:     nthConfig.DeleteLocalData,
		Timeout:             time.Duration(nthConfig.NodeTerminationGracePeriod) * time.Second,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
	}
}
