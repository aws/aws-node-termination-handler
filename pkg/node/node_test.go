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
	"flag"
	"os"
	"strconv"
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
	"k8s.io/kubectl/pkg/drain"
)

var nodeName = "NAME"

func resetFlagsForTest() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"cmd"}
	os.Setenv("NODE_NAME", nodeName)
}

func getDrainHelper(client *fake.Clientset) *drain.Helper {
	return &drain.Helper{
		Client:              client,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteLocalData:     true,
		Timeout:             time.Duration(120) * time.Second,
		Out:                 log.Logger,
		ErrOut:              log.Logger,
	}
}

func getNthConfig(t *testing.T) config.Config {
	nthConfig, err := config.ParseCliArgs()
	if err != nil {
		t.Error("failed to create nthConfig")
	}
	return nthConfig
}

func getNode(t *testing.T, drainHelper *drain.Helper) *node.Node {
	tNode, err := node.NewWithValues(getNthConfig(t), drainHelper, uptime.Uptime)
	if err != nil {
		t.Error("failed to create node")
	}
	return tNode
}

func TestDryRun(t *testing.T) {
	tNode, err := node.New(config.Config{DryRun: true})
	h.Ok(t, err)

	err = tNode.CordonAndDrain(nodeName)
	h.Ok(t, err)

	err = tNode.Cordon(nodeName)
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
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	tNode := getNode(t, getDrainHelper(client))
	err := tNode.CordonAndDrain(nodeName)
	h.Ok(t, err)
}

func TestDrainCordonNodeFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.CordonAndDrain(nodeName)
	h.Assert(t, true, "Failed to return error on CordonAndDrain failing to cordon node", err != nil)
}

func TestUncordonSuccess(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	tNode := getNode(t, getDrainHelper(client))
	err := tNode.Uncordon(nodeName)
	h.Ok(t, err)
}

func TestUncordonFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.Uncordon(nodeName)
	h.Assert(t, err != nil, "Failed to return error on Uncordon failing to fetch node")
}

func TestIsUnschedulableSuccess(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	tNode := getNode(t, getDrainHelper(client))
	value, err := tNode.IsUnschedulable(nodeName)
	h.Ok(t, err)
	h.Equals(t, false, value)
}

func TestIsUnschedulableFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	value, err := tNode.IsUnschedulable(nodeName)
	h.Assert(t, err != nil, "Failed to return error on IsUnschedulable failing to fetch node")
	h.Equals(t, true, value)
}

func TestMarkWithEventIDSuccess(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	tNode := getNode(t, getDrainHelper(client))
	err := tNode.MarkWithEventID(nodeName, "EventID")
	h.Ok(t, err)
}

func TestMarkWithEventIDFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.MarkWithEventID(nodeName, "EventID")
	h.Assert(t, err != nil, "Failed to return error on MarkWithEventID failing to fetch node")
}

func TestRemoveNTHLablesFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.RemoveNTHLabels(nodeName)
	h.Assert(t, err != nil, "Failed to return error on failing RemoveNTHLabels")
}

func TestGetEventIDSuccess(t *testing.T) {
	resetFlagsForTest()
	var labelValue = "bla"

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: map[string]string{"aws-node-termination-handler/event-id": labelValue},
		},
	})

	tNode := getNode(t, getDrainHelper(client))
	value, err := tNode.GetEventID(nodeName)
	h.Ok(t, err)
	h.Equals(t, labelValue, value)
}

func TestGetEventIDNoNodeFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	_, err := tNode.GetEventID(nodeName)
	h.Assert(t, err != nil, "Failed to return error on GetEventID failed to find node")
}

func TestGetEventIDNoLabelFailure(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	tNode := getNode(t, getDrainHelper(client))
	_, err := tNode.GetEventID(nodeName)
	h.Assert(t, err != nil, "Failed to return error on GetEventID failed to find label")
}

func TestMarkForUncordonAfterRebootAddActionLabelFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.MarkForUncordonAfterReboot(nodeName)
	h.Assert(t, err != nil, "Failed to return error on MarkForUncordonAfterReboot failing to add action Label")
}

func TestIsLableledWithActionFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	_, err := tNode.IsLabeledWithAction(nodeName)
	h.Assert(t, err != nil, "Failed to return error on IsLabeledWithAction failure")
}

func TestUncordonIfRebootedDefaultSuccess(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"aws-node-termination-handler/action":      "Test",
				"aws-node-termination-handler/action-time": strconv.FormatInt(time.Now().Unix(), 10),
			},
		},
	})
	tNode := getNode(t, getDrainHelper(client))
	err := tNode.UncordonIfRebooted(nodeName)
	h.Ok(t, err)
}

func TestUncordonIfRebootedNodeFetchFailure(t *testing.T) {
	resetFlagsForTest()

	tNode := getNode(t, getDrainHelper(fake.NewSimpleClientset()))
	err := tNode.UncordonIfRebooted(nodeName)
	h.Assert(t, err != nil, "Failed to return error on UncordonIfReboted failure to find node")
}

func TestUncordonIfRebootedTimeParseFailure(t *testing.T) {
	resetFlagsForTest()

	client := fake.NewSimpleClientset()
	client.CoreV1().Nodes().Create(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"aws-node-termination-handler/action":      "UncordonAfterReboot",
				"aws-node-termination-handler/action-time": "Something not time",
			},
		},
	})
	tNode := getNode(t, getDrainHelper(client))
	err := tNode.UncordonIfRebooted(nodeName)
	h.Assert(t, err != nil, "Failed to return error on UncordonIfReboted failure to parse time")
}
