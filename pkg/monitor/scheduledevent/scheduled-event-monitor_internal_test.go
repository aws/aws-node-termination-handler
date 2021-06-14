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

package scheduledevent

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubectl/pkg/drain"
)

var nodeName = "NAME"

func getDrainHelper(client *fake.Clientset) *drain.Helper {
	return &drain.Helper{
		Client:              client,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             time.Duration(120) * time.Second,
		Out:                 log.Logger,
		ErrOut:              log.Logger,
	}
}

func getNode(t *testing.T, drainHelper *drain.Helper) *node.Node {
	nthConfig := config.Config{
		NodeName: nodeName,
	}
	tNode, err := node.NewWithValues(nthConfig, drainHelper, uptime.Uptime)
	if err != nil {
		t.Error("failed to create node")
	}
	return tNode
}

func TestUncordonAfterRebootPreDrainSuccess(t *testing.T) {
	drainEvent := monitor.InterruptionEvent{
		EventID: "some-id-that-is-very-long-for-some-reason-and-is-definitely-over-63-characters",
	}
	nthConfig := config.Config{
		DryRun:   true,
		NodeName: nodeName,
	}

	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(context.Background(), &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}, metav1.CreateOptions{})
	h.Ok(t, err)

	tNode, err := node.NewWithValues(nthConfig, getDrainHelper(client), uptime.Uptime)
	h.Ok(t, err)

	err = uncordonAfterRebootPreDrain(drainEvent, *tNode)

	h.Ok(t, err)
}

func TestUncordonAfterRebootPreDrainMarkWithEventIDFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := uncordonAfterRebootPreDrain(monitor.InterruptionEvent{}, *tNode)
	h.Assert(t, err != nil, "Failed to return error on MarkWithEventID failing to fetch node")
}

func TestUncordonAfterRebootPreDrainNodeAlreadyMarkedSuccess(t *testing.T) {
	nthConfig := config.Config{
		DryRun:   true,
		NodeName: nodeName,
	}

	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
			Spec: v1.NodeSpec{
				Unschedulable: true,
			},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)

	tNode, err := node.NewWithValues(nthConfig, getDrainHelper(client), uptime.Uptime)
	h.Ok(t, err)

	err = uncordonAfterRebootPreDrain(monitor.InterruptionEvent{}, *tNode)
	h.Ok(t, err)
}
