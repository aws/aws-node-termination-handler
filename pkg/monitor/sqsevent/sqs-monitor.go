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
	"errors"
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
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
	// ASGTagName is the name of the instance tag whose value is the AutoScaling group name
	ASGTagName = "aws:autoscaling:groupName"
)

// ErrNodeStateNotRunning forwards condition that the instance is terminated thus metadata missing
var ErrNodeStateNotRunning = errors.New("node metadata unavailable")

// SQSMonitor is a struct definition that knows how to process events from Amazon EventBridge
type SQSMonitor struct {
	InterruptionChan        chan<- monitor.InterruptionEvent
	CancelChan              chan<- monitor.InterruptionEvent
	QueueURL                string
	SQS                     sqsiface.SQSAPI
	ASG                     autoscalingiface.AutoScalingAPI
	EC2                     ec2iface.EC2API
	CheckIfManaged          bool
	AssumeAsgTagPropagation bool
	ManagedAsgTag           string
}

// Convenience wrapper for handling a pair of an interruption event and a related error
type InterruptionEventWrapper struct {
	InterruptionEvent *monitor.InterruptionEvent
	Err               error
}

// Convenience wrapper for handling a pair of an interruption event and a related error
type InterruptionEventWrapper struct {
	InterruptionEvent *monitor.InterruptionEvent
	Err               error
}

// Kind denotes the kind of event that is processed
func (m SQSMonitor) Kind() string {
	return SQSTerminateKind
}

// Monitor continuously monitors SQS for events and coordinates processing of the events
func (m SQSMonitor) Monitor() error {
	log.Debug().Msg("Checking for queue messages")
	messages, err := m.receiveQueueMessages(m.QueueURL)
	if err != nil {
		return err
	}

	failedEventBridgeEvents := 0
	for _, message := range messages {
		eventBridgeEvent, err := m.processSQSMessage(message)
		if err != nil {
			log.Err(err).Msg("error processing SQS message")
			failedEventBridgeEvents++
			continue
		}

		interruptionEventWrappers := m.processEventBridgeEvent(eventBridgeEvent, message)

		err = m.processInterruptionEvents(interruptionEventWrappers, message)
		if err != nil {
			log.Err(err).Msg("error processing interruption events")
			failedEventBridgeEvents++
		}
	}

	if len(messages) > 0 && failedEventBridgeEvents == len(messages) {
		return fmt.Errorf("none of the waiting queue events could be processed")
	}

	return nil
}

// processSQSMessage interprets an SQS message and returns an EventBridge event
func (m SQSMonitor) processSQSMessage(message *sqs.Message) (*EventBridgeEvent, error) {
	event := EventBridgeEvent{}
	err := json.Unmarshal([]byte(*message.Body), &event)

	return &event, err
}

// processEventBridgeEvent processes an EventBridge event and returns interruption event wrappers
func (m SQSMonitor) processEventBridgeEvent(eventBridgeEvent *EventBridgeEvent, message *sqs.Message) []InterruptionEventWrapper {
	interruptionEventWrappers := []InterruptionEventWrapper{}
	interruptionEvent := &monitor.InterruptionEvent{}
	var err error

	switch eventBridgeEvent.Source {
	case "aws.autoscaling":
		interruptionEvent, err = m.asgTerminationToInterruptionEvent(eventBridgeEvent, message)
		return append(interruptionEventWrappers, InterruptionEventWrapper{interruptionEvent, err})

	case "aws.ec2":
		if eventBridgeEvent.DetailType == "EC2 Instance State-change Notification" {
			interruptionEvent, err = m.ec2StateChangeToInterruptionEvent(eventBridgeEvent, message)
		} else if eventBridgeEvent.DetailType == "EC2 Spot Instance Interruption Warning" {
			interruptionEvent, err = m.spotITNTerminationToInterruptionEvent(eventBridgeEvent, message)
		} else if eventBridgeEvent.DetailType == "EC2 Instance Rebalance Recommendation" {
			interruptionEvent, err = m.rebalanceRecommendationToInterruptionEvent(eventBridgeEvent, message)
		}
		return append(interruptionEventWrappers, InterruptionEventWrapper{interruptionEvent, err})

	case "aws.health":
		if eventBridgeEvent.DetailType == "AWS Health Event" {
			interruptionEventWrappers = m.scheduledEventToInterruptionEvents(eventBridgeEvent, message)
			return interruptionEventWrappers
		}
	}

	err = fmt.Errorf("event source (%s) is not supported", eventBridgeEvent.Source)
	return append(interruptionEventWrappers, InterruptionEventWrapper{nil, err})
}

// processInterruptionEvents takes interruption event wrappers and sends interruption events to the passed-in channel
func (m SQSMonitor) processInterruptionEvents(interruptionEventWrappers []InterruptionEventWrapper, message *sqs.Message) error {
	dropMessageSuggestionCount := 0
	failedInterruptionEventsCount := 0

	for _, eventWrapper := range interruptionEventWrappers {
		switch {
		case errors.Is(eventWrapper.Err, ErrNodeStateNotRunning):
			// If the node is no longer running, just log and delete the message
			log.Warn().Err(eventWrapper.Err).Msg("dropping interruption event for an already terminated node")
			dropMessageSuggestionCount++

		case eventWrapper.Err != nil:
			// Log errors and record as failed events. Don't delete the message in order to allow retries
			log.Err(eventWrapper.Err).Msg("ignoring interruption event due to error")
			failedInterruptionEventsCount++ // seems useless

		case eventWrapper.InterruptionEvent == nil:
			log.Debug().Msg("dropping non-actionable interruption event")
			dropMessageSuggestionCount++

		case m.CheckIfManaged && !eventWrapper.InterruptionEvent.IsManaged:
			// This event isn't for an instance that is managed by this process
			log.Debug().Str("instance-id", eventWrapper.InterruptionEvent.InstanceID).Msg("dropping interruption event for unmanaged node")
			dropMessageSuggestionCount++

		case eventWrapper.InterruptionEvent.Kind == SQSTerminateKind:
			// Successfully processed SQS message into a SQSTerminateKind interruption event
			log.Debug().Msgf("Sending %s interruption event to the interruption channel", SQSTerminateKind)
			m.InterruptionChan <- *eventWrapper.InterruptionEvent

		default:
			eventJSON, _ := json.MarshalIndent(eventWrapper.InterruptionEvent, " ", "    ")
			log.Warn().Msgf("dropping interruption event of an unrecognized kind: %s", eventJSON)
			dropMessageSuggestionCount++
		}
	}

	if dropMessageSuggestionCount == len(interruptionEventWrappers) {
		// All interruption events weren't actionable, just delete the message. If message deletion fails, count it as an error
		errs := m.deleteMessages([]*sqs.Message{message})
		if len(errs) > 0 {
			log.Err(errs[0]).Msg("Error deleting message from SQS")
			failedInterruptionEventsCount++
		}
	}

	if failedInterruptionEventsCount != 0 {
		return fmt.Errorf("some interruption events for message Id %b could not be processed", message.MessageId)
	} else {
		return nil
	}
}

// receiveQueueMessages checks the configured SQS queue for new messages
func (m SQSMonitor) receiveQueueMessages(qURL string) ([]*sqs.Message, error) {
	result, err := m.SQS.ReceiveMessage(&sqs.ReceiveMessageInput{
		AttributeNames: []*string{
			aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
		},
		MessageAttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
		QueueUrl:            &qURL,
		MaxNumberOfMessages: aws.Int64(10),
		VisibilityTimeout:   aws.Int64(20), // 20 seconds
		WaitTimeSeconds:     aws.Int64(20), // Max long polling
	})

	if err != nil {
		return nil, err
	}

	return result.Messages, nil
}

// deleteMessages deletes messages from the configured SQS queue
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
		log.Debug().Msgf("SQS Deleted Message: %s", message)
	}
	return errs
}

// NodeInfo is relevant information about a single node
type NodeInfo struct {
	AsgName    string
	InstanceID string
	IsManaged  bool
	Name       string
	Tags       map[string]string
}

// getNodeInfo returns the NodeInfo record for the given instanceID.
//
// The data is retrieved from the EC2 API.
func (m SQSMonitor) getNodeInfo(instanceID string) (*NodeInfo, error) {
	result, err := m.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID.NotFound" {
			log.Warn().Msgf("No instance found with instance-id %s", instanceID)
			return nil, ErrNodeStateNotRunning
		}
		return nil, err
	}
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		log.Warn().Msgf("No instance found with instance-id %s", instanceID)
		return nil, ErrNodeStateNotRunning
	}

	instance := result.Reservations[0].Instances[0]
	instanceJSON, _ := json.MarshalIndent(*instance, " ", "    ")
	log.Debug().Msgf("Got instance data from ec2 describe call: %s", instanceJSON)

	if *instance.PrivateDnsName == "" {
		state := "unknown"
		// safe access instance.State potentially being nil
		if instance.State != nil {
			state = *instance.State.Name
		}
		// anything except running might not contain PrivateDnsName
		if state != ec2.InstanceStateNameRunning {
			return nil, fmt.Errorf("node: '%s' in state '%s': %w", instanceID, state, ErrNodeStateNotRunning)
		}
		return nil, fmt.Errorf("unable to retrieve PrivateDnsName name for '%s' in state '%s'", instanceID, state)
	}

	nodeInfo := &NodeInfo{
		Name:       *instance.PrivateDnsName,
		InstanceID: instanceID,
		Tags:       make(map[string]string),
		IsManaged:  true,
	}
	for _, t := range (*instance).Tags {
		nodeInfo.Tags[*t.Key] = *t.Value
		if *t.Key == ASGTagName {
			nodeInfo.AsgName = *t.Value
		}
	}

	if nodeInfo.AsgName == "" && !m.AssumeAsgTagPropagation {
		// If ASG tags are not propagated we might need to use the API
		// to retrieve the ASG name
		nodeInfo.AsgName, err = m.retrieveAutoScalingGroupName(nodeInfo.InstanceID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve AutoScaling group: %w", err)
		}
	}

	if m.CheckIfManaged && nodeInfo.Tags[m.ManagedAsgTag] == "" {
		if m.AssumeAsgTagPropagation {
			nodeInfo.IsManaged = false
		} else {
			// if ASG tags are not propagated we might have to check the ASG directly
			nodeInfo.IsManaged, err = m.isASGManaged(nodeInfo.AsgName, nodeInfo.InstanceID)
			if err != nil {
				return nil, err
			}
		}
	}
	infoJSON, _ := json.MarshalIndent(nodeInfo, " ", "    ")
	log.Debug().Msgf("Got node info from AWS: %s", infoJSON)

	return nodeInfo, nil
}

// isASGManaged returns whether the autoscaling group should be managed by node termination handler
func (m SQSMonitor) isASGManaged(asgName string, instanceID string) (bool, error) {
	if asgName == "" {
		return false, nil
	}
	asgFilter := autoscaling.Filter{Name: aws.String("auto-scaling-group"), Values: []*string{aws.String(asgName)}}
	asgDescribeTagsInput := autoscaling.DescribeTagsInput{
		Filters: []*autoscaling.Filter{&asgFilter},
	}
	isManaged := false
	err := m.ASG.DescribeTagsPages(&asgDescribeTagsInput, func(resp *autoscaling.DescribeTagsOutput, next bool) bool {
		for _, tag := range resp.Tags {
			if *tag.Key == m.ManagedAsgTag {
				isManaged = true
				// breaks paging loop
				return false
			}
		}
		// continue paging loop
		return true
	})

	log.Debug().
		Str("instance_id", instanceID).
		Str("tag_key", m.ManagedAsgTag).
		Bool("is_managed", isManaged).
		Msg("directly checked if instance's Auto Scaling Group is managed")
	return isManaged, err
}

// retrieveAutoScalingGroupName returns the autoscaling group name for a given instanceID
func (m SQSMonitor) retrieveAutoScalingGroupName(instanceID string) (string, error) {
	asgDescribeInstanceInput := autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []*string{&instanceID},
		MaxRecords:  aws.Int64(50),
	}
	asgs, err := m.ASG.DescribeAutoScalingInstances(&asgDescribeInstanceInput)
	if err != nil {
		return "", err
	}
	if len(asgs.AutoScalingInstances) == 0 {
		log.Debug().Str("instance_id", instanceID).Msg("Did not find an Auto Scaling Group for the given instance id")
		return "", nil
	}
	asgName := asgs.AutoScalingInstances[0].AutoScalingGroupName
	log.Debug().
		Str("instance_id", instanceID).
		Str("asg_name", *asgName).
		Msg("performed API lookup of instance ASG")
	return *asgName, nil
}
