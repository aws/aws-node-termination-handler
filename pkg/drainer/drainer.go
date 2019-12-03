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

var drainHelper *drain.Helper
var node *corev1.Node

//InitDrainer will ensure the drainer has all the resources necessary to complete a drain
func InitDrainer(nthConfig config.Config) {
	if drainHelper == nil {
		drainHelper = getDrainHelper(nthConfig)
	}
	node = &corev1.Node{}
	var err error
	if !nthConfig.DryRun {
		node, err = drainHelper.Client.CoreV1().Nodes().Get(nthConfig.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Couldn't get node %q: %s\n", nthConfig.NodeName, err.Error())
		}
		log.Printf("Successufully retrieved node: %s", node.Name)
	}
}

//Drain will cordon the node and evict pods based on the config
func Drain(nthConfig config.Config) {
	if drainHelper == nil || node == nil {
		InitDrainer(nthConfig)
	}
	var err error
	if nthConfig.DryRun {
		log.Printf("Node %s would have been cordoned, but dry-run flag was set", nthConfig.NodeName)
	} else {
		err = drain.RunCordonOrUncordon(drainHelper, node, true)
		if err != nil {
			log.Fatalf("Couldn't cordon node %q: %s\n", node, err.Error())
		}
	}

	if nthConfig.DryRun {
		log.Printf("Node %s would have been drained, but dry-run flag was set", nthConfig.NodeName)
	} else {
		// Delete all pods on the node
		err = drain.RunNodeDrain(drainHelper, nthConfig.NodeName)
		if err != nil {
			log.Fatalln(err.Error())
		}
	}
}

func getDrainHelper(nthConfig config.Config) *drain.Helper {
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
