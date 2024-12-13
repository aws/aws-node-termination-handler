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

	"github.com/aws/aws-node-termination-handler/pkg/logging"
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

	"go.uber.org/multierr"
)

const (
	// SQSMonitorKind is a const to define this monitor kind
	SQSMonitorKind = "SQS_MONITOR"
	// ASGTagName is the name of the instance tag whose value is the AutoScaling group name
	ASGTagName                        = "aws:autoscaling:groupName"
	ASGTerminatingLifecycleTransition = "autoscaling:EC2_INSTANCE_TERMINATING"
	ASGLaunchingLifecycleTransition   = "autoscaling:EC2_INSTANCE_LAUNCHING"
)

// SQSMonitor is a struct definition that knows how to process events from Amazon EventBridge
type SQSMonitor struct {
	InterruptionChan              chan<- monitor.InterruptionEvent
	CancelChan                    chan<- monitor.InterruptionEvent
	QueueURL                      string
	SQS                           sqsiface.SQSAPI
	ASG                           autoscalingiface.AutoScalingAPI
	EC2                           ec2iface.EC2API
	CheckIfManaged                bool
	ManagedTag                    string
	BeforeCompleteLifecycleAction func()
}

// InterruptionEventWrapper is a convenience wrapper for associating an interruption event with its error, if any
type InterruptionEventWrapper struct {
	InterruptionEvent *monitor.InterruptionEvent
	Err               error
}

// Used to skip processing an error, but acknowledge an error occured during a termination event
type skip struct {
	err error
}

func (s skip) Error() string {
	return s.err.Error()
}

func (s skip) Unwrap() error {
	return s.err
}

// Kind denotes the kind of monitor
func (m SQSMonitor) Kind() string {
	return SQSMonitorKind
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
			var s skip
			if errors.As(err, &s) {
				log.Warn().Err(s).Msg("skip processing SQS message")
			} else {
				log.Err(err).Msg("error processing SQS message")
				failedEventBridgeEvents++
			}
			continue
		}

		interruptionEventWrappers := m.processEventBridgeEvent(eventBridgeEvent, message)

		if err = m.processInterruptionEvents(interruptionEventWrappers, message); err != nil {
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

	if err != nil {
		return &event, err
	}

	if len(event.DetailType) == 0 {
		event, err = m.processLifecycleEventFromASG(message)
	}

	return &event, err
}

func parseLifecycleEvent(message string) (LifecycleDetail, error) {
	lifecycleEventMessage := LifecycleDetailMessage{}
	lifecycleEvent := LifecycleDetail{}
	err := json.Unmarshal([]byte(message), &lifecycleEventMessage)
	if err != nil {
		return lifecycleEvent, fmt.Errorf("unmarshalling SQS message: %w", err)
	}
	// Converts escaped JSON object to string, to lifecycle event
	if lifecycleEventMessage.Message != nil {
		err = json.Unmarshal([]byte(fmt.Sprintf("%v", lifecycleEventMessage.Message)), &lifecycleEvent)
		if err != nil {
			err = fmt.Errorf("unmarshalling message body from '.Message': %w", err)
		}
	} else {
		err = json.Unmarshal([]byte(fmt.Sprintf("%v", message)), &lifecycleEvent)
		if err != nil {
			err = fmt.Errorf("unmarshalling message body: %w", err)
		}
	}
	return lifecycleEvent, err
}

// processLifecycleEventFromASG checks for a Lifecycle event from ASG to SQS, and wraps it in an EventBridgeEvent
func (m SQSMonitor) processLifecycleEventFromASG(message *sqs.Message) (EventBridgeEvent, error) {
	log.Debug().Interface("message", message).Msg("processing lifecycle event from ASG")
	eventBridgeEvent := EventBridgeEvent{}

	if message == nil {
		return eventBridgeEvent, fmt.Errorf("ASG event message is nil")
	}
	lifecycleEvent, err := parseLifecycleEvent(*message.Body)

	switch {
	case err != nil:
		return eventBridgeEvent, fmt.Errorf("parsing lifecycle event messsage from ASG: %w", err)

	case lifecycleEvent.Event == TEST_NOTIFICATION || lifecycleEvent.LifecycleTransition == TEST_NOTIFICATION:
		err := fmt.Errorf("message is a test notification")
		if errs := m.deleteMessages([]*sqs.Message{message}); errs != nil {
			err = multierr.Append(err, errs[0])
		}
		return eventBridgeEvent, skip{err}

	case lifecycleEvent.LifecycleTransition != ASGTerminatingLifecycleTransition &&
		lifecycleEvent.LifecycleTransition != ASGLaunchingLifecycleTransition:
		return eventBridgeEvent, fmt.Errorf("lifecycle transition must be %s or %s. Got %s", ASGTerminatingLifecycleTransition, ASGLaunchingLifecycleTransition, lifecycleEvent.LifecycleTransition)
	}

	eventBridgeEvent.Source = "aws.autoscaling"
	eventBridgeEvent.Time = lifecycleEvent.Time
	eventBridgeEvent.ID = lifecycleEvent.RequestID
	eventBridgeEvent.Detail, err = json.Marshal(lifecycleEvent)
	return eventBridgeEvent, err
}

// processEventBridgeEvent processes an EventBridge event and returns interruption event wrappers
func (m SQSMonitor) processEventBridgeEvent(eventBridgeEvent *EventBridgeEvent, message *sqs.Message) []InterruptionEventWrapper {
	interruptionEventWrappers := []InterruptionEventWrapper{}
	interruptionEvent := &monitor.InterruptionEvent{}
	var err error

	if eventBridgeEvent == nil {
		return append(interruptionEventWrappers, InterruptionEventWrapper{nil, fmt.Errorf("eventBridgeEvent is nil")})
	}
	if message == nil {
		return append(interruptionEventWrappers, InterruptionEventWrapper{nil, fmt.Errorf("message is nil")})
	}

	switch eventBridgeEvent.Source {
	case "aws.autoscaling":
		lifecycleEvent := LifecycleDetail{}
		err = json.Unmarshal([]byte(eventBridgeEvent.Detail), &lifecycleEvent)
		if err != nil {
			interruptionEvent, err = nil, fmt.Errorf("unmarshaling message, %s, from ASG lifecycle event: %w", *message.MessageId, err)
			interruptionEventWrappers = append(interruptionEventWrappers, InterruptionEventWrapper{interruptionEvent, err})
		}
		if lifecycleEvent.LifecycleTransition == ASGLaunchingLifecycleTransition {
			interruptionEvent, err = m.createAsgInstanceLaunchEvent(eventBridgeEvent, message)
			interruptionEventWrappers = append(interruptionEventWrappers, InterruptionEventWrapper{interruptionEvent, err})
		} else if lifecycleEvent.LifecycleTransition == ASGTerminatingLifecycleTransition {
			interruptionEvent, err = m.asgTerminationToInterruptionEvent(eventBridgeEvent, message)
			interruptionEventWrappers = append(interruptionEventWrappers, InterruptionEventWrapper{interruptionEvent, err})
		}
		return interruptionEventWrappers

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
			return m.scheduledEventToInterruptionEvents(eventBridgeEvent, message)
		}
	}

	err = fmt.Errorf("event source (%s) is not supported", eventBridgeEvent.Source)
	return append(interruptionEventWrappers, InterruptionEventWrapper{nil, err})
}

// processInterruptionEvents takes interruption event wrappers and sends events to the interruption channel
func (m SQSMonitor) processInterruptionEvents(interruptionEventWrappers []InterruptionEventWrapper, message *sqs.Message) error {
	dropMessageSuggestionCount := 0
	failedInterruptionEventsCount := 0
	var skipErr skip

	for _, eventWrapper := range interruptionEventWrappers {
		switch {
		case errors.As(eventWrapper.Err, &skipErr):
			log.Warn().Err(skipErr).Msg("dropping event")
			dropMessageSuggestionCount++

		case eventWrapper.Err != nil:
			// Log errors and record as failed events. Don't delete the message in order to allow retries
			log.Err(eventWrapper.Err).Msg("ignoring interruption event due to error")
			failedInterruptionEventsCount++

		case eventWrapper.InterruptionEvent == nil:
			log.Debug().Msg("dropping non-actionable interruption event")
			dropMessageSuggestionCount++

		case m.CheckIfManaged && !eventWrapper.InterruptionEvent.IsManaged:
			// This event is for an instance that is not managed by this process
			log.Debug().Str("instance-id", eventWrapper.InterruptionEvent.InstanceID).Msg("dropping interruption event for unmanaged node")
			dropMessageSuggestionCount++

		case eventWrapper.InterruptionEvent.Monitor == SQSMonitorKind:
			// Successfully processed SQS message into a eventWrapper.InterruptionEvent.Kind interruption event
			logging.VersionedMsgs.SendingInterruptionEventToChannel(eventWrapper.InterruptionEvent.Kind)
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
		return fmt.Errorf("some interruption events for message Id %s could not be processed", *message.MessageId)
	}

	return nil
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

// completeLifecycleAction completes the lifecycle action after calling the "before" hook.
func (m SQSMonitor) completeLifecycleAction(input *autoscaling.CompleteLifecycleActionInput) (*autoscaling.CompleteLifecycleActionOutput, error) {
	if m.BeforeCompleteLifecycleAction != nil {
		m.BeforeCompleteLifecycleAction()
	}
	return m.ASG.CompleteLifecycleAction(input)
}

// NodeInfo is relevant information about a single node
type NodeInfo struct {
	AsgName      string
	InstanceID   string
	ProviderID   string
	InstanceType string
	IsManaged    bool
	Name         string
	Tags         map[string]string
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
		// handle all kinds of InvalidInstanceID error events
		// - https://docs.aws.amazon.com/AWSEC2/latest/APIReference/errors-overview.html
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID" {
			msg := fmt.Sprintf("Invalid instance id %s provided", instanceID)
			log.Warn().Msg(msg)
			return nil, skip{fmt.Errorf("%s", msg)}
		}
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID.NotFound" {
			msg := fmt.Sprintf("No instance found with instance-id %s", instanceID)
			log.Warn().Msg(msg)
			return nil, skip{fmt.Errorf("%s", msg)}
		}
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID.Malformed" {
			msg := fmt.Sprintf("Malformed instance-id %s", instanceID)
			log.Warn().Msg(msg)
			return nil, skip{fmt.Errorf("%s", msg)}
		}
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID.NotLinkable" {
			msg := fmt.Sprintf("Instance-id %s not linkable", instanceID)
			log.Warn().Msg(msg)
			return nil, skip{fmt.Errorf("%s", msg)}
		}
		return nil, err
	}
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		msg := fmt.Sprintf("No reservation with instance-id %s", instanceID)
		log.Warn().Msg(msg)
		return nil, skip{fmt.Errorf("%s", msg)}
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
			return nil, skip{fmt.Errorf("node: '%s' in state '%s'", instanceID, state)}
		}
		return nil, fmt.Errorf("unable to retrieve PrivateDnsName name for '%s' in state '%s'", instanceID, state)
	}

	providerID := ""
	if *instance.Placement.AvailabilityZone != "" {
		providerID = fmt.Sprintf("aws:///%s/%s", *instance.Placement.AvailabilityZone, instanceID)
	}

	nodeInfo := &NodeInfo{
		Name:         *instance.PrivateDnsName,
		InstanceID:   instanceID,
		ProviderID:   providerID,
		InstanceType: *instance.InstanceType,
		Tags:         make(map[string]string),
		IsManaged:    true,
	}
	for _, t := range (*instance).Tags {
		nodeInfo.Tags[*t.Key] = *t.Value
		if *t.Key == ASGTagName {
			nodeInfo.AsgName = *t.Value
		}
	}

	if m.CheckIfManaged {
		if _, ok := nodeInfo.Tags[m.ManagedTag]; !ok {
			nodeInfo.IsManaged = false
		}
	}

	infoJSON, _ := json.MarshalIndent(nodeInfo, " ", "    ")
	log.Debug().Msgf("Got node info from AWS: %s", infoJSON)

	return nodeInfo, nil
}
