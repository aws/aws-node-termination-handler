// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package sqsevent_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
)

var spotItnEvent = sqsevent.EventBridgeEvent{
	Version:    "0",
	ID:         "1e5527d7-bb36-4607-3370-4164db56a40e",
	DetailType: "EC2 Spot Instance Interruption Warning",
	Source:     "aws.ec2",
	Account:    "123456789012",
	Time:       "1970-01-01T00:00:00Z",
	Region:     "us-east-1",
	Resources: []string{
		"arn:aws:ec2:us-east-1b:instance/i-0b662ef9931388ba0",
	},
	Detail: []byte(`{
		"instance-id": "i-0b662ef9931388ba0",
		"instance-action": "terminate"
	}`),
}

var asgLifecycleEvent = sqsevent.EventBridgeEvent{
	Version:    "0",
	ID:         "782d5b4c-0f6f-1fd6-9d62-ecf6aed0a470",
	DetailType: "EC2 Instance-terminate Lifecycle Action",
	Source:     "aws.autoscaling",
	Account:    "123456789012",
	Time:       "2020-07-01T22:19:58Z",
	Region:     "us-east-1",
	Resources: []string{
		"arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:26e7234b-03a4-47fb-b0a9-2b241662774e:autoScalingGroupName/nth-test1",
	},
	Detail: []byte(`{
		"LifecycleActionToken": "0befcbdb-6ecd-498a-9ff7-ae9b54447cd6",
		"AutoScalingGroupName": "nth-test1",
		"LifecycleHookName": "node-termination-handler",
		"EC2InstanceId": "i-0633ac2b0d9769723",
		"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
	  }`),
}

var asgLaunchLifecycleEvent = sqsevent.EventBridgeEvent{
	Version:    "0",
	ID:         "83c632dd-0145-1ab0-ae93-a756ebf429b5",
	DetailType: "EC2 Instance-launch Lifecycle Action",
	Source:     "aws.autoscaling",
	Account:    "123456789012",
	Time:       "2020-07-01T22:30:58Z",
	Region:     "us-east-1",
	Resources: []string{
		"arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:c4c64181-52c1-dd3f-20bb-f4a0965a09db:autoScalingGroupName/nth-test1",
	},
	Detail: []byte(`{
		"LifecycleActionToken": "524632c5-3333-d52d-3992-d9633ec24ed7",
		"AutoScalingGroupName": "nth-test1",
		"LifecycleHookName": "node-termination-handler-launch",
		"EC2InstanceId": "i-0a68bf5ef13e21b52",
		"LifecycleTransition": "autoscaling:EC2_INSTANCE_LAUNCHING"
	  }`),
}

var asgLifecycleEventFromSQS = sqsevent.LifecycleDetail{
	LifecycleHookName:    "test-nth-asg-to-sqs",
	RequestID:            "3775fac9-93c3-7ead-8713-159816566000",
	LifecycleTransition:  "autoscaling:EC2_INSTANCE_TERMINATING",
	AutoScalingGroupName: "my-asg",
	Time:                 "2022-01-31T23:07:47.872Z",
	EC2InstanceID:        "i-040107f6ba000e5ee",
	LifecycleActionToken: "b4dd0f5b-0ef2-4479-9dad-6c55f027000e",
}

var asgLifecycleTestNotification = sqsevent.EventBridgeEvent{
	Version:    "0",
	ID:         "782d5b4c-0f6f-1fd6-9d62-ecf6aed0a470",
	DetailType: "EC2 Instance-terminate Lifecycle Action",
	Source:     "aws.autoscaling",
	Account:    "123456789012",
	Time:       "2020-07-01T22:19:58Z",
	Region:     "us-east-1",
	Resources: []string{
		"arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:26e7234b-03a4-47fb-b0a9-2b241662774e:autoScalingGroupName/nth-test1",
	},
	Detail: []byte(`{
		"Event": "autoscaling:TEST_NOTIFICATION",
		"LifecycleTransition": "autoscaling:TEST_NOTIFICATION"
	  }`),
}

var asgLifecycleTestNotificationFromSQS = sqsevent.LifecycleDetail{
	LifecycleHookName:    "test-nth-asg-to-sqs",
	RequestID:            "3775fac9-93c3-7ead-8713-159816566000",
	Event:                "autoscaling:TEST_NOTIFICATION",
	LifecycleTransition:  "autoscaling:TEST_NOTIFICATION",
	AutoScalingGroupName: "my-asg",
	Time:                 "2022-01-31T23:07:47.872Z",
	EC2InstanceID:        "i-040107f6ba000e5ee",
	LifecycleActionToken: "b4dd0f5b-0ef2-4479-9dad-6c55f027000e",
}

var rebalanceRecommendationEvent = sqsevent.EventBridgeEvent{
	Version:    "0",
	ID:         "5d5555d5-dd55-5555-5555-5555dd55d55d",
	DetailType: "EC2 Instance Rebalance Recommendation",
	Source:     "aws.ec2",
	Account:    "123456789012",
	Time:       "2020-10-26T14:14:14Z",
	Region:     "us-east-1",
	Resources: []string{
		"arn:aws:ec2:us-east-1b:instance/i-0b662ef9931388ba0",
	},
	Detail: []byte(`{
		"instance-id": "i-0b662ef9931388ba0"
	}`),
}

func TestMonitorKind(t *testing.T) {
	h.Assert(t, sqsevent.SQSMonitor{}.Kind() == sqsevent.SQSMonitorKind, "SQSMonitor kind should return the kind constant for the monitor")
}

func TestMonitor_EventBridgeSuccess(t *testing.T) {
	spotItnEventNoTime := spotItnEvent
	spotItnEventNoTime.Time = ""
	i := 0
	expectedResultKinds := []string{monitor.SpotITNKind, monitor.ASGLifecycleKind, monitor.SpotITNKind, monitor.RebalanceRecommendationKind}
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent, spotItnEventNoTime, rebalanceRecommendationEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
		ec2Mock := h.MockedEC2{
			DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			ManagedTag:       "aws-node-termination-handler/managed",
			ASG:              &h.MockedASG{},
			CheckIfManaged:   true,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Ok(t, err)

		select {
		case result := <-drainChan:
			h.Equals(t, expectedResultKinds[i], result.Kind)
			h.Equals(t, sqsevent.SQSMonitorKind, result.Monitor)
			h.Equals(t, result.NodeName, dnsNodeName)
			h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err = result.PostDrainTask(result, node.Node{})
			h.Ok(t, err)
		default:
			h.Ok(t, fmt.Errorf("Expected an event to be generated"))
		}
		i++
	}
}

func TestMonitor_EventBridgeTestNotification(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLifecycleTestNotification)
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case result := <-drainChan:
		h.Ok(t, fmt.Errorf("Did not expect a result on the drain channel: %#v", result))
	default:
		h.Ok(t, nil)
	}
}

func TestMonitor_AsgDirectToSqsSuccess(t *testing.T) {
	event := asgLifecycleEventFromSQS
	eventBytes, err := json.Marshal(&event)
	h.Ok(t, err)
	eventStr := string(eventBytes)
	msg := sqs.Message{Body: &eventStr}
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		ASG:              &h.MockedASG{},
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case result := <-drainChan:
		h.Equals(t, monitor.ASGLifecycleKind, result.Kind)
		h.Equals(t, sqsevent.SQSMonitorKind, result.Monitor)
		h.Equals(t, result.NodeName, dnsNodeName)
		h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
		h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
		err = result.PostDrainTask(result, node.Node{})
		h.Ok(t, err)
	default:
		h.Ok(t, fmt.Errorf("Expected an event to be generated"))
	}
}

func TestMonitor_AsgDirectToSqsTestNotification(t *testing.T) {
	eventBytes, err := json.Marshal(&asgLifecycleTestNotificationFromSQS)
	h.Ok(t, err)
	eventStr := string(eventBytes)
	msg := sqs.Message{Body: &eventStr}
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case result := <-drainChan:
		h.Ok(t, fmt.Errorf("Did not expect a result on the drain channel: %#v", result))
	default:
		h.Ok(t, nil)
	}
}

func TestMonitor_DrainTasks(t *testing.T) {
	testEvents := []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent, rebalanceRecommendationEvent}
	messages := make([]*sqs.Message, 0, len(testEvents))
	for _, event := range testEvents {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages = append(messages, &msg)
	}

	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
	}
	drainChan := make(chan monitor.InterruptionEvent, len(testEvents))

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		ASG:              asgMock,
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err := sqsMonitor.Monitor()
	h.Ok(t, err)

	i := 0
	expectedResultKinds := []string{monitor.SpotITNKind, monitor.ASGLifecycleKind, monitor.RebalanceRecommendationKind}
	for _, event := range testEvents {
		t.Run(event.DetailType, func(st *testing.T) {
			result := <-drainChan
			h.Equals(st, expectedResultKinds[i], result.Kind)
			h.Equals(st, sqsevent.SQSMonitorKind, result.Monitor)
			h.Equals(st, result.NodeName, dnsNodeName)
			h.Assert(st, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(st, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err := result.PostDrainTask(result, node.Node{})
			h.Ok(st, err)
		})
		i++
	}
}

func TestMonitor_DrainTasks_Delay(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLaunchLifecycleEvent)
	h.Ok(t, err)

	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: []*sqs.Message{&msg}},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	hookCalled := false
	sqsMonitor := sqsevent.SQSMonitor{
		SQS:                           sqsMock,
		EC2:                           ec2Mock,
		ManagedTag:                    "aws-node-termination-handler/managed",
		ASG:                           asgMock,
		CheckIfManaged:                true,
		QueueURL:                      "https://test-queue",
		InterruptionChan:              drainChan,
		BeforeCompleteLifecycleAction: func() { hookCalled = true },
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	t.Run(asgLaunchLifecycleEvent.DetailType, func(st *testing.T) {
		result := <-drainChan
		h.Equals(st, monitor.ASGLaunchLifecycleKind, result.Kind)
		h.Equals(st, sqsevent.SQSMonitorKind, result.Monitor)
		h.Equals(st, result.NodeName, dnsNodeName)
		h.Assert(st, result.PostDrainTask != nil, "PostDrainTask should have been set")
		err := result.PostDrainTask(result, node.Node{})
		h.Ok(st, err)
		h.Assert(st, hookCalled, "BeforeCompleteLifecycleAction hook not called")
	})
}

func TestMonitor_DrainTasks_Errors(t *testing.T) {
	testEvents := []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent, {}, rebalanceRecommendationEvent}
	messages := make([]*sqs.Message, 0, len(testEvents))
	for _, event := range testEvents {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages = append(messages, &msg)
	}

	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
	}
	drainChan := make(chan monitor.InterruptionEvent, len(testEvents))

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		ASG:              asgMock,
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err := sqsMonitor.Monitor()
	h.Ok(t, err)

	count := 0
	i := 0
	expectedResultKinds := []string{monitor.SpotITNKind, monitor.ASGLifecycleKind, monitor.RebalanceRecommendationKind}
	done := false
	for !done {
		select {
		case result := <-drainChan:
			count++
			h.Equals(t, expectedResultKinds[i], result.Kind)
			h.Equals(t, sqsevent.SQSMonitorKind, result.Monitor)
			h.Equals(t, result.NodeName, dnsNodeName)
			h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err := result.PostDrainTask(result, node.Node{})
			h.Ok(t, err)
		default:
			done = true
		}
		i++
	}
	h.Equals(t, count, 3)
}

func TestMonitor_DrainTasksASGFailure(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLaunchLifecycleEvent)
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
		CompleteLifecycleActionErr:  awserr.NewRequestFailure(aws.ErrMissingEndpoint, 500, "bad-request"),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		ASG:              asgMock,
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case result := <-drainChan:
		h.Equals(t, monitor.ASGLaunchLifecycleKind, result.Kind)
		h.Equals(t, sqsevent.SQSMonitorKind, result.Monitor)
		h.Equals(t, result.NodeName, dnsNodeName)
		h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
		err = result.PostDrainTask(result, node.Node{})
		h.Nok(t, err)
	default:
		h.Ok(t, fmt.Errorf("Expected to get an event with a failing post drain task"))
	}
}

func TestMonitor_Failure(t *testing.T) {
	emptyEvent := sqsevent.EventBridgeEvent{}
	for _, event := range []sqsevent.EventBridgeEvent{emptyEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Nok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestMonitor_SQSFailure(t *testing.T) {
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  fmt.Errorf("error"),
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Nok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}

	}
}

func TestMonitor_SQSNoMessages(t *testing.T) {
	messages := []*sqs.Message{}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
	}

	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}
	err := sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case <-drainChan:
		h.Ok(t, fmt.Errorf("Expected no events"))
	default:
		h.Ok(t, nil)
	}

}

// Test processing invalid sqs message
func TestMonitor_SQSJsonErr(t *testing.T) {
	replaceStr := `{"test":"test-string-to-replace"}`
	badJson := []*sqs.Message{{Body: aws.String(`?`)}}
	spotEventBadDetail := spotItnEvent
	spotEventBadDetail.Detail = []byte(replaceStr)
	badDetailsMessageSpot, err := getSQSMessageFromEvent(spotEventBadDetail)
	h.Ok(t, err)
	asgEventBadDetail := asgLifecycleEvent
	asgEventBadDetail.Detail = []byte(replaceStr)
	badDetailsMessageASG, err := getSQSMessageFromEvent(asgEventBadDetail)
	h.Ok(t, err)
	badDetailsMessageSpot.Body = aws.String(strings.Replace(*badDetailsMessageSpot.Body, replaceStr, "?", 1))
	badDetailsMessageASG.Body = aws.String(strings.Replace(*badDetailsMessageASG.Body, replaceStr, "?", 1))
	for _, badMessages := range [][]*sqs.Message{badJson, {&badDetailsMessageSpot}, {&badDetailsMessageASG}} {
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: badMessages},
			ReceiveMessageErr:  nil,
		}

		drainChan := make(chan monitor.InterruptionEvent, 1)
		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}
		err := sqsMonitor.Monitor()
		h.Nok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestMonitor_EC2Failure(t *testing.T) {
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		ec2Mock := h.MockedEC2{
			DescribeInstancesResp: getDescribeInstancesResp("", true, true),
			DescribeInstancesErr:  fmt.Errorf("error"),
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Nok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestMonitor_EC2NoInstances(t *testing.T) {
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		ec2Mock := h.MockedEC2{
			DescribeInstancesResp: ec2.DescribeInstancesOutput{},
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Ok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestMonitor_DescribeInstancesError(t *testing.T) {
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		ec2Mock := h.MockedEC2{
			DescribeInstancesResp: ec2.DescribeInstancesOutput{},
			DescribeInstancesErr:  awserr.New("InvalidInstanceID.NotFound", "The instance ID 'i-0d6bd3ce2bf8a6751' does not exist\n\tstatus code: 400, request id: 6a5c30e2-922d-464c-946c-a1ec76e5920b", fmt.Errorf("original error")),
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Ok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestMonitor_EC2NoDNSName(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLifecycleEvent)
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("", true, true),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case <-drainChan:
		h.Ok(t, fmt.Errorf("Expected no events"))
	default:
		h.Ok(t, nil)
	}
}

func TestMonitor_EC2NoDNSNameOnTerminatedInstance(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLifecycleEvent)
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
	}
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("", true, true),
	}
	ec2Mock.DescribeInstancesResp.Reservations[0].Instances[0].State = &ec2.InstanceState{
		Name: aws.String("running"),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Nok(t, err)

	select {
	case <-drainChan:
		h.Ok(t, fmt.Errorf("Expected no events"))
	default:
		h.Ok(t, nil)
	}
}

func TestMonitor_SQSDeleteFailure(t *testing.T) {
	msg, err := getSQSMessageFromEvent(asgLifecycleEvent)
	h.Ok(t, err)
	messages := []*sqs.Message{
		&msg,
	}
	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
		ReceiveMessageErr:  nil,
		DeleteMessageResp:  sqs.DeleteMessageOutput{},
		DeleteMessageErr:   fmt.Errorf("error"),
	}
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("", true, true),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedTag:       "aws-node-termination-handler/managed",
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Nok(t, err)

	select {
	case <-drainChan:
		h.Ok(t, fmt.Errorf("Expected no events"))
	default:
		h.Ok(t, nil)
	}
}

func TestMonitor_InstanceNotManaged(t *testing.T) {
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		messages := []*sqs.Message{
			&msg,
		}
		sqsMock := h.MockedSQS{
			ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: messages},
			ReceiveMessageErr:  nil,
		}
		dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
		ec2Mock := h.MockedEC2{
			DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, false),
		}

		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			CheckIfManaged:   true,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Ok(t, err)

		select {
		case <-drainChan:
			h.Ok(t, fmt.Errorf("Expected no events"))
		default:
			h.Ok(t, nil)
		}
	}
}

func TestSendHeartbeats_EarlyClosure(t *testing.T) {
	err := heartbeatTestHelper(nil, 3500, 1, 5)
	h.Ok(t, err)
	h.Assert(t, h.HeartbeatCallCount == 3, "3 Heartbeat Expected, got %d", h.HeartbeatCallCount)
}

func TestSendHeartbeats_HeartbeatUntilExpire(t *testing.T) {
	err := heartbeatTestHelper(nil, 8000, 1, 5)
	h.Ok(t, err)
	h.Assert(t, h.HeartbeatCallCount == 5, "5 Heartbeat Expected, got %d", h.HeartbeatCallCount)
}

func TestSendHeartbeats_ErrThrottlingASG(t *testing.T) {
	RecordLifecycleActionHeartbeatErr := awserr.New("Throttling", "Rate exceeded", nil)
	err := heartbeatTestHelper(RecordLifecycleActionHeartbeatErr, 8000, 1, 6)
	h.Ok(t, err)
	h.Assert(t, h.HeartbeatCallCount == 6, "6 Heartbeat Expected, got %d", h.HeartbeatCallCount)
}

func TestSendHeartbeats_ErrInvalidTarget(t *testing.T) {
	RecordLifecycleActionHeartbeatErr := awserr.New("ValidationError", "No active Lifecycle Action found", nil)
	err := heartbeatTestHelper(RecordLifecycleActionHeartbeatErr, 6000, 1, 4)
	h.Ok(t, err)
	h.Assert(t, h.HeartbeatCallCount == 1, "1 Heartbeat Expected, got %d", h.HeartbeatCallCount)
}

func heartbeatTestHelper(RecordLifecycleActionHeartbeatErr error, sleepMilliSeconds int, heartbeatInterval int, heartbeatUntil int) error {
	h.HeartbeatCallCount = 0

	msg, err := getSQSMessageFromEvent(asgLifecycleEvent)
	if err != nil {
		return err
	}

	sqsMock := h.MockedSQS{
		ReceiveMessageResp: sqs.ReceiveMessageOutput{Messages: []*sqs.Message{&msg}},
	}
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName, true, true),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp:        autoscaling.CompleteLifecycleActionOutput{},
		RecordLifecycleActionHeartbeatResp: autoscaling.RecordLifecycleActionHeartbeatOutput{},
		RecordLifecycleActionHeartbeatErr:  RecordLifecycleActionHeartbeatErr,
		HeartbeatTimeout:                   30,
	}

	drainChan := make(chan monitor.InterruptionEvent, 1)
	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ASG:              asgMock,
		InterruptionChan: drainChan,
		BeforeCompleteLifecycleAction: func() {
			time.Sleep(time.Duration(sleepMilliSeconds) * time.Millisecond)
		},
	}

	if err := sqsMonitor.Monitor(); err != nil {
		return err
	}

	nthConfig := &config.Config{
		HeartbeatInterval: heartbeatInterval,
		HeartbeatUntil:    heartbeatUntil,
	}

	testNode, _ := node.New(*nthConfig, nil)
	result := <-drainChan

	if result.PreDrainTask == nil {
		return fmt.Errorf("PreDrainTask should have been set")
	}
	if err := result.PreDrainTask(result, *testNode); err != nil {
		return err
	}

	if result.PostDrainTask == nil {
		return fmt.Errorf("PostDrainTask should have been set")
	}
	if err := result.PostDrainTask(result, *testNode); err != nil {
		return err
	}

	return nil
}

func getDescribeInstancesResp(privateDNSName string, withASGTag bool, withManagedTag bool) ec2.DescribeInstancesOutput {
	tags := []*ec2.Tag{}
	if withASGTag {
		tags = append(tags, &ec2.Tag{Key: aws.String(sqsevent.ASGTagName), Value: aws.String("test-asg")})
	}
	if withManagedTag {
		tags = append(tags, &ec2.Tag{Key: aws.String("aws-node-termination-handler/managed"), Value: aws.String("")})
	}
	return ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: []*ec2.Instance{
					{
						InstanceId: aws.String("i-0123456789"),
						Placement: &ec2.Placement{
							AvailabilityZone: aws.String("us-east-2a"),
							GroupName:        aws.String(""),
							Tenancy:          aws.String("default"),
						},
						InstanceType:   aws.String("t3.medium"),
						PrivateDnsName: &privateDNSName,
						Tags:           tags,
					},
				},
			},
		},
	}
}

func getSQSMessageFromEvent(event sqsevent.EventBridgeEvent) (sqs.Message, error) {
	eventBytes, err := json.Marshal(&event)
	if err != nil {
		return sqs.Message{}, err
	}
	eventStr := string(eventBytes)
	messageId := "d7de6634-f672-ce5c-d87e-ae0b1b5b2510"
	return sqs.Message{Body: &eventStr, MessageId: &messageId}, nil
}
