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

func TestKind(t *testing.T) {
	h.Assert(t, sqsevent.SQSMonitor{}.Kind() == sqsevent.SQSTerminateKind, "SQSMonitor kind should return the kind constant for the event")
}

func TestMonitor_Success(t *testing.T) {
	spotItnEventNoTime := spotItnEvent
	spotItnEventNoTime.Time = ""
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
			DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
		}
		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			ManagedAsgTag:    "aws-node-termination-handler/managed",
			ASG:              mockIsManagedTrue(nil),
			CheckIfManaged:   true,
			QueueURL:         "https://test-queue",
			InterruptionChan: drainChan,
		}

		err = sqsMonitor.Monitor()
		h.Ok(t, err)

		select {
		case result := <-drainChan:
			h.Equals(t, sqsevent.SQSTerminateKind, result.Kind)
			h.Equals(t, result.NodeName, dnsNodeName)
			h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err = result.PostDrainTask(result, node.Node{})
			h.Ok(t, err)
		default:
			h.Ok(t, fmt.Errorf("Expected an event to be generated"))
		}

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
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
	}
	drainChan := make(chan monitor.InterruptionEvent, len(testEvents))

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(&asgMock),
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err := sqsMonitor.Monitor()
	h.Ok(t, err)

	for _, event := range testEvents {
		t.Run(event.DetailType, func(st *testing.T) {
			result := <-drainChan
			h.Equals(st, sqsevent.SQSTerminateKind, result.Kind)
			h.Equals(st, result.NodeName, dnsNodeName)
			h.Assert(st, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(st, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err := result.PostDrainTask(result, node.Node{})
			h.Ok(st, err)
		})
	}
}

func TestMonitor_DrainTasks_Errors(t *testing.T) {
	testEvents := []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent, sqsevent.EventBridgeEvent{}, rebalanceRecommendationEvent}
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
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
	}
	drainChan := make(chan monitor.InterruptionEvent, len(testEvents))

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(&asgMock),
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err := sqsMonitor.Monitor()
	h.Ok(t, err)

	count := 0
	done := false
	for !done {
		select {
		case result := <-drainChan:
			count++
			h.Equals(t, sqsevent.SQSTerminateKind, result.Kind)
			h.Equals(t, result.NodeName, dnsNodeName)
			h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
			h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
			err := result.PostDrainTask(result, node.Node{})
			h.Ok(t, err)
		default:
			done = true
		}
	}
	h.Equals(t, count, 3)
}

func TestMonitor_DrainTasksASGFailure(t *testing.T) {
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
	dnsNodeName := "ip-10-0-0-157.us-east-2.compute.internal"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
	}
	asgMock := h.MockedASG{
		CompleteLifecycleActionResp: autoscaling.CompleteLifecycleActionOutput{},
		CompleteLifecycleActionErr:  awserr.NewRequestFailure(aws.ErrMissingEndpoint, 500, "bad-request"),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(&asgMock),
		CheckIfManaged:   true,
		QueueURL:         "https://test-queue",
		InterruptionChan: drainChan,
	}

	err = sqsMonitor.Monitor()
	h.Ok(t, err)

	select {
	case result := <-drainChan:
		h.Equals(t, sqsevent.SQSTerminateKind, result.Kind)
		h.Equals(t, result.NodeName, dnsNodeName)
		h.Assert(t, result.PostDrainTask != nil, "PostDrainTask should have been set")
		h.Assert(t, result.PreDrainTask != nil, "PreDrainTask should have been set")
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
			DescribeInstancesResp: getDescribeInstancesResp(""),
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
		h.Nok(t, err)

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
		DescribeInstancesResp: getDescribeInstancesResp(""),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(nil),
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
		DescribeInstancesResp: getDescribeInstancesResp(""),
	}
	ec2Mock.DescribeInstancesResp.Reservations[0].Instances[0].State = &ec2.InstanceState{
		Name: aws.String("running"),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(nil),
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
		DescribeInstancesResp: getDescribeInstancesResp(""),
	}
	drainChan := make(chan monitor.InterruptionEvent, 1)

	sqsMonitor := sqsevent.SQSMonitor{
		SQS:              sqsMock,
		EC2:              ec2Mock,
		ManagedAsgTag:    "aws-node-termination-handler/managed",
		ASG:              mockIsManagedTrue(nil),
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
			DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
		}

		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			ASG:              mockIsManagedFalse(nil),
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

func TestMonitor_InstanceManagedErr(t *testing.T) {
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
			DescribeInstancesResp: getDescribeInstancesResp(dnsNodeName),
		}

		drainChan := make(chan monitor.InterruptionEvent, 1)

		sqsMonitor := sqsevent.SQSMonitor{
			SQS:              sqsMock,
			EC2:              ec2Mock,
			ASG:              mockIsManagedErr(nil),
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
}

// AWS Mock Helpers specific to sqs-monitor tests

func getDescribeInstancesResp(privateDNSName string) ec2.DescribeInstancesOutput {
	return ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: []*ec2.Instance{
					{
						InstanceId:     aws.String("i-0123456789"),
						PrivateDnsName: &privateDNSName,
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
	return sqs.Message{Body: &eventStr}, nil
}

func mockIsManagedTrue(asg *h.MockedASG) h.MockedASG {
	if asg == nil {
		asg = &h.MockedASG{}
	}
	asg.DescribeAutoScalingInstancesResp = autoscaling.DescribeAutoScalingInstancesOutput{
		AutoScalingInstances: []*autoscaling.InstanceDetails{
			{AutoScalingGroupName: aws.String("test-asg")},
		},
	}
	asg.DescribeTagsPagesResp = autoscaling.DescribeTagsOutput{
		Tags: []*autoscaling.TagDescription{
			{Key: aws.String("aws-node-termination-handler/managed")},
		},
	}
	return *asg
}

func mockIsManagedFalse(asg *h.MockedASG) h.MockedASG {
	if asg == nil {
		asg = &h.MockedASG{}
	}
	asg.DescribeAutoScalingInstancesResp = autoscaling.DescribeAutoScalingInstancesOutput{
		AutoScalingInstances: []*autoscaling.InstanceDetails{},
	}
	return *asg
}

func mockIsManagedErr(asg *h.MockedASG) h.MockedASG {
	if asg == nil {
		asg = &h.MockedASG{}
	}
	asg.DescribeAutoScalingInstancesErr = fmt.Errorf("error")
	return *asg
}
