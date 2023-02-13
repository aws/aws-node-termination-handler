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

package node_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/drain"
)

// Size of the fakeRecorder buffer
const recorderBufferSize = 10

var nodeName = "NAME"

func getDrainHelper(client *fake.Clientset) *drain.Helper {
	return &drain.Helper{
		Ctx:                 context.TODO(),
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

func TestDryRun(t *testing.T) {
	tNode, err := node.New(config.Config{DryRun: true})
	h.Ok(t, err)

	fakeRecorder := record.NewFakeRecorder(recorderBufferSize)
	defer close(fakeRecorder.Events)

	err = tNode.CordonAndDrain(nodeName, "cordonReason", fakeRecorder)

	h.Ok(t, err)

	err = tNode.Cordon(nodeName, "cordonReason")
	h.Ok(t, err)

	err = tNode.Uncordon(nodeName)
	h.Ok(t, err)

	_, err = tNode.IsUnschedulable(nodeName)
	h.Ok(t, err)

	err = tNode.MarkWithEventID(nodeName, "eventID")
	h.Ok(t, err)

	_, err = tNode.GetEventID(nodeName)
	h.Ok(t, err)

	err = tNode.RemoveNTHLabels(nodeName)
	h.Ok(t, err)

	err = tNode.MarkForUncordonAfterReboot(nodeName)
	h.Ok(t, err)

	_, err = tNode.IsLabeledWithAction(nodeName)
	h.Ok(t, err)

	err = tNode.UncordonIfRebooted(nodeName)
	h.Ok(t, err)
}

func TestNewFailure(t *testing.T) {
	_, err := node.New(config.Config{})
	h.Assert(t, true, "Failed to return error when creating new Node.", err != nil)
}

func TestDrainSuccess(t *testing.T) {
	isOwnerController := true
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(
		context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)

	_, err = client.CoreV1().Pods("default").Create(
		context.Background(),
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cool-app-pod-",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Name:       "cool-app",
						Kind:       "ReplicaSet",
						Controller: &isOwnerController,
					},
				},
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
			},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)

	fakeRecorder := record.NewFakeRecorder(recorderBufferSize)

	drainHelper := getDrainHelper(client)
	drainHelper.DisableEviction = true
	tNode := getNode(t, drainHelper)
	err = tNode.CordonAndDrain(nodeName, "cordonReason", fakeRecorder)
	close(fakeRecorder.Events)
	h.Ok(t, err)
	expectedEventArrived := false
	for event := range fakeRecorder.Events {
		if strings.Contains(event, "Normal PodEviction Pod evicted due to node drain") {
			expectedEventArrived = true
		}
	}
	h.Assert(t, expectedEventArrived, "PodEvicted event was not emitted")
}

func TestDrainCordonNodeFailure(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(recorderBufferSize)
	defer close(fakeRecorder.Events)
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.CordonAndDrain(nodeName, "cordonReason", fakeRecorder)
	h.Assert(t, true, "Failed to return error on CordonAndDrain failing to cordon node", err != nil)
}

func TestUncordonSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(
		context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	err = tNode.Uncordon(nodeName)
	h.Ok(t, err)
}

func TestUncordonFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.Uncordon(nodeName)
	h.Assert(t, err != nil, "Failed to return error on Uncordon failing to fetch node")
}

func TestIsUnschedulableSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(
		context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	value, err := tNode.IsUnschedulable(nodeName)
	h.Ok(t, err)
	h.Equals(t, false, value)
}

func TestIsUnschedulableFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	value, err := tNode.IsUnschedulable(nodeName)
	h.Assert(t, err != nil, "Failed to return error on IsUnschedulable failing to fetch node")
	h.Equals(t, true, value)
}

func TestMarkWithEventIDSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(
		context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	err = tNode.MarkWithEventID(nodeName, "EventID")
	h.Ok(t, err)
}

func TestMarkWithEventIDFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.MarkWithEventID(nodeName, "EventID")
	h.Assert(t, err != nil, "Failed to return error on MarkWithEventID failing to fetch node")
}

func TestRemoveNTHLablesFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.RemoveNTHLabels(nodeName)
	h.Assert(t, err != nil, "Failed to return error on failing RemoveNTHLabels")
}

func TestGetEventIDSuccess(t *testing.T) {
	var labelValue = "bla"

	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName,
				Labels: map[string]string{"aws-node-termination-handler/event-id": labelValue},
			},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	value, err := tNode.GetEventID(nodeName)
	h.Ok(t, err)
	h.Equals(t, labelValue, value)
}

func TestGetEventIDNoNodeFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	_, err := tNode.GetEventID(nodeName)
	h.Assert(t, err != nil, "Failed to return error on GetEventID failed to find node")
}

func TestGetEventIDNoLabelFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(
		context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	_, err = tNode.GetEventID(nodeName)
	h.Assert(t, err != nil, "Failed to return error on GetEventID failed to find label")
}

func TestMarkForUncordonAfterRebootAddActionLabelFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.MarkForUncordonAfterReboot(nodeName)
	h.Assert(t, err != nil, "Failed to return error on MarkForUncordonAfterReboot failing to add action Label")
}

func TestFetchPodsNameList(t *testing.T) {
	client := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "myPod",
				Labels: map[string]string{
					"spec.nodeName": nodeName,
				},
			},
		},
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		},
	)

	tNode := getNode(t, getDrainHelper(client))
	podList, err := tNode.FetchPodNameList(nodeName)
	h.Ok(t, err)
	h.Equals(t, []string{"myPod"}, podList)
}

func TestLogPods(t *testing.T) {
	client := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "myPod",
				Labels: map[string]string{
					"spec.nodeName": nodeName,
				},
			},
		},
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		},
	)

	tNode := getNode(t, getDrainHelper(client))
	err := tNode.LogPods([]string{"myPod"}, nodeName)
	h.Ok(t, err)
}

func TestIsLableledWithActionFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	_, err := tNode.IsLabeledWithAction(nodeName)
	h.Assert(t, err != nil, "Failed to return error on IsLabeledWithAction failure")
}

func TestUncordonIfRebootedDefaultSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					"aws-node-termination-handler/action":      "Test",
					"aws-node-termination-handler/action-time": strconv.FormatInt(time.Now().Unix(), 10),
				},
			},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	err = tNode.UncordonIfRebooted(nodeName)
	h.Ok(t, err)
}

func TestUncordonIfRebootedNodeFetchFailure(t *testing.T) {
	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.UncordonIfRebooted(nodeName)
	h.Assert(t, err != nil, "Failed to return error on UncordonIfReboted failure to find node")
}

func TestUncordonIfRebootedTimeParseFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := client.CoreV1().Nodes().Create(context.Background(),
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					"aws-node-termination-handler/action":      "UncordonAfterReboot",
					"aws-node-termination-handler/action-time": "Something not time",
				},
			},
		},
		metav1.CreateOptions{})
	h.Ok(t, err)
	tNode := getNode(t, getDrainHelper(client))
	err = tNode.UncordonIfRebooted(nodeName)
	h.Assert(t, err != nil, "Failed to return error on UncordonIfReboted failure to parse time")
}
