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

type Drainer struct {
	nthConfig   config.Config
	drainHelper *drain.Helper
}

// InitDrainer will ensure the drainer has all the resources necessary to complete a drain.
// This function must be called before any function in the drainer package.
func New(nthConfig config.Config) (*Drainer, error) {
	drainHelper, err := getDrainHelper(nthConfig)
	if err != nil {
		log.Println("Unable to construct a drainer because a kubernetes drainHelper could not be built: ", err)
		return &Drainer{}, err
	}
	return &Drainer{
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
		return &drain.Helper{}, err
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		log.Println("Failed to create kubernetes clientset: ", err)
		return &drain.Helper{}, err
	}
	drainHelper.Client = clientset

	return drainHelper, nil
}

// FetchNode will send an http request to the k8s api server and return the corev1 model node
func (d Drainer) FetchNode() (*corev1.Node, error) {
	node := &corev1.Node{}
	if d.nthConfig.DryRun {
		return node, nil
	}
	return d.drainHelper.Client.CoreV1().Nodes().Get(d.nthConfig.NodeName, metav1.GetOptions{})
}

//Drain will cordon the node and evict pods based on the config
func (d Drainer) Drain() error {
	if d.nthConfig.DryRun {
		log.Printf("Node %s would have been cordoned and drained, but dry-run flag was set\n", d.nthConfig.NodeName)
		return nil
	}
	err := d.cordonNode()
	if err != nil {
		log.Println("There was an error in the drain process while cordoning the node: ", err)
		return err
	}
	// Delete all pods on the node
	err = drain.RunNodeDrain(d.drainHelper, d.nthConfig.NodeName)
	if err != nil {
		log.Println("There was an error in the drain process while evicting pods: ", err)
		return err
	}
	return nil
}

// Cordon will add a NoSchedule on the node
func (d Drainer) cordonNode() error {
	node, err := d.FetchNode()
	if err != nil {
		log.Println("There was an error which fetching the kubernetes node: ", err)
		return err
	}
	err = drain.RunCordonOrUncordon(d.drainHelper, node, true)
	if err != nil {
		log.Printf("Couldn't cordon node %q: %v\n", node, err)
		return err
	}
	return nil
}
