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

package interruptionevent

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubectl/pkg/drain"
)

var spotNodeName = "NAME"

func getSpotDrainHelper(client *fake.Clientset) *drain.Helper {
	return &drain.Helper{
		Client:              client,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteLocalData:     true,
		Timeout:             time.Duration(120) * time.Second,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
	}
}

func TestSetInterruptionTaint(t *testing.T) {
	drainEvent := InterruptionEvent{
		EventID: "some-id",
	}
	nthConfig := config.Config{
		DryRun:   true,
		NodeName: spotNodeName,
	}

	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: spotNodeName}})
	h.Ok(t, err)

	tNode, err := node.NewWithValues(nthConfig, getSpotDrainHelper(client))

	err = setInterruptionTaint(drainEvent, *tNode)

	n, _ := client.CoreV1().Nodes().Get(spotNodeName, metav1.GetOptions{})
	h.Assert(t, n.Spec.Taints[0].Key == node.SpotInterruptionTaint, fmt.Sprintf("Missing expected taint key %s", node.SpotInterruptionTaint))
	h.Assert(t, n.Spec.Taints[0].Value == drainEvent.EventID, fmt.Sprintf("Missing expected taint value %s", drainEvent.EventID))
	h.Assert(t, n.Spec.Taints[0].Effect == v1.TaintEffectNoSchedule, fmt.Sprintf("Missing expected taint effect %s", v1.TaintEffectNoSchedule))

	h.Ok(t, err)
}
