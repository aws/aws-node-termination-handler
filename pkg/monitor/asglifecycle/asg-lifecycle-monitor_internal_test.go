// Copyright 2020 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package asglifecycle

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

const nodeName = "NAME"

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

func TestSetInterruptionTaint(t *testing.T) {
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

	err = setInterruptionTaint(drainEvent, *tNode)

	h.Ok(t, err)
}

func TestInterruptionTaintAlreadyPresent(t *testing.T) {
	drainEvent := monitor.InterruptionEvent{
		EventID: "some-id-that-is-very-long-for-some-reason-and-is-definitely-over-63-characters",
	}
	nthConfig := config.Config{
		DryRun:   false,
		NodeName: nodeName,
	}

	client := fake.NewSimpleClientset()
	newNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		Spec: v1.NodeSpec{Taints: []v1.Taint{{
			Key:    node.ASGLifecycleTerminationTaint,
			Value:  drainEvent.EventID[:63],
			Effect: v1.TaintEffectNoSchedule,
		},
		}},
	}

	_, err := client.CoreV1().Nodes().Create(context.Background(), newNode, metav1.CreateOptions{})
	h.Ok(t, err)

	tNode, err := node.NewWithValues(nthConfig, getDrainHelper(client), uptime.Uptime)
	h.Ok(t, err)

	err = setInterruptionTaint(drainEvent, *tNode)

	h.Ok(t, err)
}
