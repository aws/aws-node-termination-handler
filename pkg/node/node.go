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

package node

import (
	"log"
	"os"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
)

// Node represents a kubernetes node with functions to manipulate its state via the kubernetes api server
type Node struct {
	nthConfig   config.Config
	drainHelper *drain.Helper
}

// New will construct a node struct to perform various node function through the kubernetes api server
func New(nthConfig config.Config) (*Node, error) {
	drainHelper, err := getDrainHelper(nthConfig)
	if err != nil {
		log.Println("Unable to construct a drainer because a kubernetes drainHelper could not be built: ", err)
		return nil, err
	}
	return &Node{
		nthConfig:   nthConfig,
		drainHelper: drainHelper,
	}, nil
}

func getDrainHelper(nthConfig config.Config) (*drain.Helper, error) {
	drainHelper := &drain.Helper{
		Client:              &kubernetes.Clientset{},
		Force:               true,
		GracePeriodSeconds:  nthConfig.PodTerminationGracePeriod,
		IgnoreAllDaemonSets: nthConfig.IgnoreDaemonSets,
		DeleteLocalData:     nthConfig.DeleteLocalData,
		Timeout:             time.Duration(nthConfig.NodeTerminationGracePeriod) * time.Second,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
	}
	if nthConfig.DryRun {
		return drainHelper, nil
	}

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Println("Failed to create in-cluster config: ", err)
		return nil, err
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		log.Println("Failed to create kubernetes clientset: ", err)
		return nil, err
	}
	drainHelper.Client = clientset

	return drainHelper, nil
}

// FetchNode will send an http request to the k8s api server and return the corev1 model node
func (n Node) FetchNode() (*corev1.Node, error) {
	node := &corev1.Node{}
	if n.nthConfig.DryRun {
		return node, nil
	}
	return n.drainHelper.Client.CoreV1().Nodes().Get(n.nthConfig.NodeName, metav1.GetOptions{})
}

//Drain will cordon the node and evict pods based on the config
func (n Node) Drain() error {
	if n.nthConfig.DryRun {
		log.Printf("Node %s would have been cordoned and drained, but dry-run flag was set\n", n.nthConfig.NodeName)
		return nil
	}
	err := n.cordonNode()
	if err != nil {
		log.Println("There was an error in the drain process while cordoning the node: ", err)
		return err
	}
	// Delete all pods on the node
	err = drain.RunNodeDrain(n.drainHelper, n.nthConfig.NodeName)
	if err != nil {
		log.Println("There was an error in the drain process while evicting pods: ", err)
		return err
	}
	return nil
}

// Cordon will add a NoSchedule on the node
func (n Node) cordonNode() error {
	node, err := n.FetchNode()
	if err != nil {
		log.Println("There was an error fetching the node in preparation for cordoning: ", err)
		return err
	}
	err = drain.RunCordonOrUncordon(n.drainHelper, node, true)
	if err != nil {
		log.Printf("Couldn't cordon node %q: %v\n", node, err)
		return err
	}
	return nil
}

// UncordonNode will remove the NoSchedule on the node
func (n Node) UncordonNode() error {
	if n.nthConfig.DryRun {
		log.Printf("Node %s would have been uncordoned, but dry-run flag was set", n.nthConfig.NodeName)
		return nil
	}
	node, err := n.FetchNode()
	if err != nil {
		log.Println("There was an error fetching the node in preparation for uncordoning: ", err)
		return err
	}
	err = drain.RunCordonOrUncordon(n.drainHelper, node, false)
	if err != nil {
		log.Printf("Couldn't uncordon node %q: %s\n", node, err)
		return err
	}
	return nil
}
