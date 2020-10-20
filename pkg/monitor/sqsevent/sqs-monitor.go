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

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-sdk-go/aws"
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
	// NTHManagedASG is the ASG tag key to determine if NTH is managing the ASG
	NTHManagedASG = "aws-node-termination-handler/managed"
)

// SQSMonitor is a struct definition that knows how to process events from Amazon EventBridge
type SQSMonitor struct {
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	QueueURL         string
	SQS              sqsiface.SQSAPI
	ASG              autoscalingiface.AutoScalingAPI
	EC2              ec2iface.EC2API
	CheckIfManaged   bool
}

// Kind denotes the kind of event that is processed
func (m SQSMonitor) Kind() string {
	return SQSTerminateKind
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
		if event.DetailType == "EC2 Instance State-change Notification" {
			interruptionEvent, err = m.ec2StateChangeToInterruptionEvent(event, messages)
		} else if event.DetailType == "EC2 Spot Instance Interruption Warning" {
			interruptionEvent, err = m.spotITNTerminationToInterruptionEvent(event, messages)
		} else if event.DetailType == "EC2 Instance Rebalance Recommendation" {
			interruptionEvent, err = m.rebalanceNoticeToInterruptionEvent(event, messages)
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
		MaxNumberOfMessages: aws.Int64(2),
		VisibilityTimeout:   aws.Int64(20), // 20 seconds
		WaitTimeSeconds:     aws.Int64(0),
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
		log.Log().Msgf("SQS Deleted Message: %s", message)
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
		return "", err
	}
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		return "", fmt.Errorf("No instance found with instance-id %s", instanceID)
	}

	instance := result.Reservations[0].Instances[0]
	log.Debug().Msgf("Got nodename from private ip %s", *instance.PrivateDnsName)
	instanceJSON, _ := json.MarshalIndent(*instance, " ", "    ")
	log.Debug().Msgf("Got nodename from ec2 describe call: %s", instanceJSON)
	return *instance.PrivateDnsName, nil
}

// isInstanceManaged returns whether the instance specified should be managed by node termination handler
func (m SQSMonitor) isInstanceManaged(instanceID string) (bool, error) {
	if instanceID == "" {
		return false, fmt.Errorf("Instance ID was empty when calling isInstanceManaged")
	}
	asgDescribeInstanceInput := autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []*string{&instanceID},
		MaxRecords:  aws.Int64(50),
	}
	asgs, err := m.ASG.DescribeAutoScalingInstances(&asgDescribeInstanceInput)
	if err != nil {
		return false, err
	}
	if len(asgs.AutoScalingInstances) == 0 {
		log.Debug().Str("instance_id", instanceID).Msg("Did not find an Auto Scaling Group for the given instance id")
		return false, nil
	}
	asgName := asgs.AutoScalingInstances[0].AutoScalingGroupName
	asgFilter := autoscaling.Filter{Name: aws.String("auto-scaling-group"), Values: []*string{asgName}}
	asgDescribeTagsInput := autoscaling.DescribeTagsInput{
		Filters: []*autoscaling.Filter{&asgFilter},
	}
	isManaged := false
	err = m.ASG.DescribeTagsPages(&asgDescribeTagsInput, func(resp *autoscaling.DescribeTagsOutput, next bool) bool {
		for _, tag := range resp.Tags {
			if *tag.Key == NTHManagedASG {
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
			Msgf("The instance's Auto Scaling Group is not tagged as managed with tag key: %s", NTHManagedASG)
	}
	return isManaged, err
}
