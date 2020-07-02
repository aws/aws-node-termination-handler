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

package sqsevent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/rs/zerolog/log"
)

const (
	// SQSTerminateKind is a const to define an SQS termination kind of interruption event
	SQSTerminateKind = "SQS_TERMINATE"
)

// Example SQS ASG Lifecycle Termination Event Message:
// {
//   "version": "0",
//   "id": "782d5b4c-0f6f-1fd6-9d62-ecf6aed0a470",
//   "detail-type": "EC2 Instance-terminate Lifecycle Action",
//   "source": "aws.autoscaling",
//   "account": "896453262834",
//   "time": "2020-07-01T22:19:58Z",
//   "region": "us-east-1",
//   "resources": [
//     "arn:aws:autoscaling:us-east-1:896453262834:autoScalingGroup:26e7234b-03a4-47fb-b0a9-2b241662774e:autoScalingGroupName/testt1.demo-0a20f32c.kops.sh"
//   ],
//   "detail": {
//     "LifecycleActionToken": "0befcbdb-6ecd-498a-9ff7-ae9b54447cd6",
//     "AutoScalingGroupName": "testt1.demo-0a20f32c.kops.sh",
//     "LifecycleHookName": "cluster-termination-handler",
//     "EC2InstanceId": "i-0633ac2b0d9769723",
//     "LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
//   }
// }

// Example Spot ITN Event:
// {
// 	"version": "0",
// 	"id": "1e5527d7-bb36-4607-3370-4164db56a40e",
// 	"detail-type": "EC2 Spot Instance Interruption Warning",
// 	"source": "aws.ec2",
// 	"account": "123456789012",
// 	"time": "1970-01-01T00:00:00Z",
// 	"region": "us-east-1",
// 	"resources": [
// 	  "arn:aws:ec2:us-east-1b:instance/i-0b662ef9931388ba0"
// 	],
// 	"detail": {
// 	  "instance-id": "i-0b662ef9931388ba0",
// 	  "instance-action": "terminate"
// 	}
// }

// EventBridgeEvent is a structure to hold generic event details from Amazon EventBridge
type EventBridgeEvent struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Account    string          `json:"account"`
	Time       string          `json:"time"`
	Region     string          `json:"region"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

func (e EventBridgeEvent) getTime() time.Time {
	terminationTime, err := time.Parse(time.RFC3339, e.Time)
	if err != nil {
		return time.Now()
	}
	return terminationTime
}

// LifecycleDetail provides the ASG lifecycle event details
type LifecycleDetail struct {
	LifecycleActionToken string
	AutoScalingGroupName string
	LifecycleHookName    string
	EC2InstanceID        string `json:"EC2InstanceId"`
	LifecycleTransition  string
}

// SpotInterruptionDetail holds the event details for spot interruption events from Amazon EventBridge
type SpotInterruptionDetail struct {
	InstanceID     string `json:"instance-id"`
	InstanceAction string `json:"instance-action"`
}

// SQSMonitor is a struct definiiton that knows how to process events from Amazon EventBridge
type SQSMonitor struct {
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	QueueURL         string
	SQS              sqsiface.SQSAPI
	ASG              autoscalingiface.AutoScalingAPI
	EC2              ec2iface.EC2API
}

// Monitor continuously monitors SQS for events and sends interruption events to the passed in channel
func (m SQSMonitor) Monitor() error {
	interruptionEvent, err := m.checkForSQSMessage()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == SQSTerminateKind {
		log.Debug().Msgf("Sending %s interruption event to the interruption channel", SQSTerminateKind)
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

// Kind denotes the kind of event that is processed
func (m SQSMonitor) Kind() string {
	return SQSTerminateKind
}

// checkForSpotInterruptionNotice checks sqs for new messages and returns interruption events
func (m SQSMonitor) checkForSQSMessage() (*monitor.InterruptionEvent, error) {

	log.Debug().Msg("Checking for queue messages")
	messages, err := m.receiveQueueMessages(m.QueueURL)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	event := EventBridgeEvent{}
	err = json.Unmarshal([]byte(*messages[0].Body), &event)
	if err != nil {
		return nil, err
	}

	interruptionEvent := monitor.InterruptionEvent{}

	switch event.Source {
	case "aws.autoscaling":
		interruptionEvent, err = m.asgTerminationToInterruptionEvent(event, messages)
		if err != nil {
			return nil, err
		}
	case "aws.ec2":
		interruptionEvent, err = m.spotITNTerminationToInterruptionEvent(event, messages)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("Event source (%s) is not supported", event.Source)
	}

	return &interruptionEvent, err
}

func (m SQSMonitor) asgTerminationToInterruptionEvent(event EventBridgeEvent, messages []*sqs.Message) (monitor.InterruptionEvent, error) {
	lifecycleDetail := &LifecycleDetail{}
	err := json.Unmarshal(event.Detail, lifecycleDetail)
	if err != nil {
		return monitor.InterruptionEvent{}, err
	}

	nodeName, err := m.retrieveNodeName(lifecycleDetail.EC2InstanceID)
	if err != nil {
		return monitor.InterruptionEvent{}, err
	}

	interruptionEvent := monitor.InterruptionEvent{
		EventID:     fmt.Sprintf("asg-lifecycle-term-%x", event.ID),
		Kind:        SQSTerminateKind,
		StartTime:   event.getTime(),
		NodeName:    nodeName,
		Description: fmt.Sprintf("ASG Lifecycle Termination event received. Instance will be interrupted at %s \n", event.getTime()),
	}

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, _ node.Node) error {
		resp, err := m.ASG.CompleteLifecycleAction(&autoscaling.CompleteLifecycleActionInput{
			AutoScalingGroupName:  &lifecycleDetail.AutoScalingGroupName,
			LifecycleActionResult: aws.String("CONTINUE"),
			LifecycleHookName:     &lifecycleDetail.LifecycleHookName,
			LifecycleActionToken:  &lifecycleDetail.LifecycleActionToken,
			InstanceId:            &lifecycleDetail.EC2InstanceID,
		})
		if err != nil {
			if aerr, ok := err.(awserr.RequestFailure); ok && aerr.StatusCode() != 400 {
				return err
			}
		}
		log.Log().Msgf("Completed ASG Lifecycle Hook: %s", resp)
		errs := m.deleteMessages(messages)
		if errs != nil {
			return errs[0]
		}
		return nil
	}

	if nodeName == "" {
		log.Log().Msg("Node name is empty, assuming instance was already terminated, deleting queue message")
		errs := m.deleteMessages(messages)
		if errs != nil {
			log.Warn().Msgf("There was an error deleting the message: %v", errs[0])
		}
	}
	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintSpotItn(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Warn().Msgf("Unable to taint node with taint %s:%s: %v", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID, err)
		}
		return nil
	}

	return interruptionEvent, nil
}

func (m SQSMonitor) spotITNTerminationToInterruptionEvent(event EventBridgeEvent, messages []*sqs.Message) (monitor.InterruptionEvent, error) {
	spotInterruptionDetail := &SpotInterruptionDetail{}
	err := json.Unmarshal(event.Detail, spotInterruptionDetail)
	if err != nil {
		return monitor.InterruptionEvent{}, err
	}

	nodeName, err := m.retrieveNodeName(spotInterruptionDetail.InstanceID)
	if err != nil {
		return monitor.InterruptionEvent{}, err
	}

	interruptionEvent := monitor.InterruptionEvent{
		EventID:     fmt.Sprintf("spot-itn-event-%x", event.ID),
		Kind:        SQSTerminateKind,
		StartTime:   event.getTime(),
		NodeName:    nodeName,
		Description: fmt.Sprintf("Spot Interruption event received. Instance will be interrupted at %s \n", event.getTime()),
	}
	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		errs := m.deleteMessages([]*sqs.Message{messages[0]})
		if errs != nil {
			return errs[0]
		}
		return nil
	}
	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintSpotItn(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Log().Msgf("Unable to taint node with taint %s:%s: %v", node.SpotInterruptionTaint, interruptionEvent.EventID, err)
		}
		return nil
	}
	return interruptionEvent, nil
}

func (m SQSMonitor) receiveQueueMessages(qURL string) ([]*sqs.Message, error) {
	result, err := m.SQS.ReceiveMessage(&sqs.ReceiveMessageInput{
		AttributeNames: []*string{
			aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
		},
		MessageAttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
		QueueUrl:            &qURL,
		MaxNumberOfMessages: aws.Int64(2),
		VisibilityTimeout:   aws.Int64(20), // 20 seconds
		WaitTimeSeconds:     aws.Int64(0),
	})

	if err != nil {
		return nil, err
	}

	return result.Messages, nil
}

func (m SQSMonitor) deleteMessages(messages []*sqs.Message) []error {
	var errs []error
	for _, message := range messages {
		_, err := m.SQS.DeleteMessage(&sqs.DeleteMessageInput{
			ReceiptHandle: message.ReceiptHandle,
			QueueUrl:      &m.QueueURL,
		})
		if err != nil {
			errs = append(errs, err)
		}
		log.Log().Msgf("SQS Deleted Message: %s", message)
	}
	return errs
}

func (m SQSMonitor) retrieveNodeName(instanceID string) (string, error) {
	result, err := m.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	})
	if err != nil {
		log.Warn().Msgf("could not describe node name from ec2 api: %v", err)
		return "", err
	}
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		log.Warn().Msg("could not find node from ec2 api")
		return "", fmt.Errorf("No instance found with instance-id %s", instanceID)
	}

	instance := result.Reservations[0].Instances[0]
	log.Debug().Msgf("Got nodename from private ip %s", *instance.PrivateDnsName)
	instanceJSON, _ := json.MarshalIndent(*instance, " ", "    ")
	log.Debug().Msgf("Got nodename from ec2 describe call: %s", instanceJSON)
	return *instance.PrivateDnsName, nil
}
