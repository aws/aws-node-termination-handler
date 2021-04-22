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
)

// ErrNodeStateNotRunning forwards condition that the instance is terminated thus metadata missing
var ErrNodeStateNotRunning = errors.New("node metadata unavailable")

// SQSMonitor is a struct definition that knows how to process events from Amazon EventBridge
type SQSMonitor struct {
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	QueueURL         string
	SQS              sqsiface.SQSAPI
	ASG              autoscalingiface.AutoScalingAPI
	EC2              ec2iface.EC2API
	CheckIfManaged   bool
	ManagedAsgTag    string
}

// Kind denotes the kind of event that is processed
func (m SQSMonitor) Kind() string {
	return SQSTerminateKind
}

// Monitor continuously monitors SQS for events and sends interruption events to the passed in channel
func (m SQSMonitor) Monitor() error {
	log.Debug().Msg("Checking for queue messages")
	messages, err := m.receiveQueueMessages(m.QueueURL)
	if err != nil {
		return err
	}

	failedEvents := 0
	for _, message := range messages {
		interruptionEvent, err := m.processSQSMessage(message)
		switch {
		case errors.Is(err, ErrNodeStateNotRunning):
			// If the node is no longer running, just log and delete the message.  If message deletion fails, count it as an error.
			log.Err(err).Msg("dropping event for an already terminated node")
			errs := m.deleteMessages([]*sqs.Message{message})
			if len(errs) > 0 {
				log.Err(errs[0]).Msg("error deleting event for already terminated node")
				failedEvents++
			}

		case err != nil:
			// Log errors and record as failed events
			log.Err(err).Msg("ignoring event due to error")
			failedEvents++

		case err == nil && interruptionEvent != nil && interruptionEvent.Kind == SQSTerminateKind:
			// Successfully processed SQS message into a SQSTerminateKind interruption event
			log.Debug().Msgf("Sending %s interruption event to the interruption channel", SQSTerminateKind)
			m.InterruptionChan <- *interruptionEvent
		}
	}

	if len(messages) > 0 && failedEvents == len(messages) {
		return fmt.Errorf("none of the waiting queue events could be processed")
	}

	return nil
}

// processSQSMessage checks sqs for new messages and returns interruption events
func (m SQSMonitor) processSQSMessage(message *sqs.Message) (*monitor.InterruptionEvent, error) {
	event := EventBridgeEvent{}
	err := json.Unmarshal([]byte(*message.Body), &event)
	if err != nil {
		return nil, err
	}

	interruptionEvent := monitor.InterruptionEvent{}

	switch event.Source {
	case "aws.autoscaling":
		interruptionEvent, err = m.asgTerminationToInterruptionEvent(event, message)
		if err != nil {
			return nil, err
		}
	case "aws.ec2":
		if event.DetailType == "EC2 Instance State-change Notification" {
			interruptionEvent, err = m.ec2StateChangeToInterruptionEvent(event, message)
		} else if event.DetailType == "EC2 Spot Instance Interruption Warning" {
			interruptionEvent, err = m.spotITNTerminationToInterruptionEvent(event, message)
		} else if event.DetailType == "EC2 Instance Rebalance Recommendation" {
			interruptionEvent, err = m.rebalanceRecommendationToInterruptionEvent(event, message)
		}
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("Event source (%s) is not supported", event.Source)
	}

	// Bail if empty event is returned after parsing
	if interruptionEvent.EventID == "" {
		return nil, nil
	}

	if m.CheckIfManaged {
		isManaged, err := m.isInstanceManaged(interruptionEvent.InstanceID)
		if err != nil {
			return &interruptionEvent, err
		}
		if !isManaged {
			return nil, nil
		}
	}

	return &interruptionEvent, err
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
		MaxNumberOfMessages: aws.Int64(5),
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

// retrieveNodeName queries the EC2 API to determine the private DNS name for the instanceID specified
func (m SQSMonitor) retrieveNodeName(instanceID string) (string, error) {
	result, err := m.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "InvalidInstanceID.NotFound" {
			log.Warn().Msgf("No instance found with instance-id %s", instanceID)
			return "", ErrNodeStateNotRunning
		}
		return "", err
	}
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		log.Warn().Msgf("No instance found with instance-id %s", instanceID)
		return "", ErrNodeStateNotRunning
	}

	instance := result.Reservations[0].Instances[0]
	nodeName := *instance.PrivateDnsName
	log.Debug().Msgf("Got nodename from private ip %s", nodeName)
	instanceJSON, _ := json.MarshalIndent(*instance, " ", "    ")
	log.Debug().Msgf("Got instance data from ec2 describe call: %s", instanceJSON)

	if nodeName == "" {
		state := "unknown"
		// safe access instance.State potentially being nil
		if instance.State != nil {
			state = *instance.State.Name
		}
		// anything except running might not contain PrivateDnsName
		if state != ec2.InstanceStateNameRunning {
			return "", fmt.Errorf("node: '%s' in state '%s': %w", instanceID, state, ErrNodeStateNotRunning)
		}
		return "", fmt.Errorf("unable to retrieve PrivateDnsName name for '%s' in state '%s'", instanceID, state)
	}
	return nodeName, nil
}

// isInstanceManaged returns whether the instance specified should be managed by node termination handler
func (m SQSMonitor) isInstanceManaged(instanceID string) (bool, error) {
	if instanceID == "" {
		return false, fmt.Errorf("Instance ID was empty when calling isInstanceManaged")
	}
	asgName, err := m.retrieveAutoScalingGroupName(instanceID)
	if asgName == "" {
		return false, err
	}
	asgFilter := autoscaling.Filter{Name: aws.String("auto-scaling-group"), Values: []*string{aws.String(asgName)}}
	asgDescribeTagsInput := autoscaling.DescribeTagsInput{
		Filters: []*autoscaling.Filter{&asgFilter},
	}
	isManaged := false
	err = m.ASG.DescribeTagsPages(&asgDescribeTagsInput, func(resp *autoscaling.DescribeTagsOutput, next bool) bool {
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

	if !isManaged {
		log.Debug().
			Str("instance_id", instanceID).
			Msgf("The instance's Auto Scaling Group is not tagged as managed with tag key: %s", m.ManagedAsgTag)
	}
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
	return *asgName, err
}
