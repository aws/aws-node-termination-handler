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
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/rs/zerolog/log"
)

/* Example SQS ASG Lifecycle Termination Event Message:
{
  "version": "0",
  "id": "782d5b4c-0f6f-1fd6-9d62-ecf6aed0a470",
  "detail-type": "EC2 Instance-terminate Lifecycle Action",
  "source": "aws.autoscaling",
  "account": "123456789012",
  "time": "2020-07-01T22:19:58Z",
  "region": "us-east-1",
  "resources": [
    "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:26e7234b-03a4-47fb-b0a9-2b241662774e:autoScalingGroupName/testt1.demo-0a20f32c.kops.sh"
  ],
  "detail": {
    "LifecycleActionToken": "0befcbdb-6ecd-498a-9ff7-ae9b54447cd6",
    "AutoScalingGroupName": "testt1.demo-0a20f32c.kops.sh",
    "LifecycleHookName": "cluster-termination-handler",
    "EC2InstanceId": "i-0633ac2b0d9769723",
    "LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
  }
}
*/

const TEST_NOTIFICATION = "autoscaling:TEST_NOTIFICATION"

type LifecycleDetailMessage struct {
	Message interface{} `json:"Message"`
}

// LifecycleDetail provides the ASG lifecycle event details
type LifecycleDetail struct {
	LifecycleActionToken string `json:"LifecycleActionToken"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	LifecycleHookName    string `json:"LifecycleHookName"`
	EC2InstanceID        string `json:"EC2InstanceId"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	Event                string `json:"Event"`
	RequestID            string `json:"RequestId"`
	Time                 string `json:"Time"`
}

func (m SQSMonitor) asgTerminationToInterruptionEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	lifecycleDetail := &LifecycleDetail{}
	err := json.Unmarshal(event.Detail, lifecycleDetail)
	if err != nil {
		return nil, err
	}

	if lifecycleDetail.Event == TEST_NOTIFICATION || lifecycleDetail.LifecycleTransition == TEST_NOTIFICATION {
		return nil, skip{fmt.Errorf("message is an ASG test notification")}
	}

	nodeInfo, err := m.getNodeInfo(lifecycleDetail.EC2InstanceID)
	if err != nil {
		return nil, err
	}

	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("asg-lifecycle-term-%x", event.ID),
		Kind:                 monitor.ASGLifecycleKind,
		Monitor:              SQSMonitorKind,
		AutoScalingGroupName: lifecycleDetail.AutoScalingGroupName,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		InstanceID:           lifecycleDetail.EC2InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		InstanceType:         nodeInfo.InstanceType,
		Description:          fmt.Sprintf("ASG Lifecycle Termination event received. Instance will be interrupted at %s \n", event.getTime()),
	}

	stopHeartbeatCh := make(chan struct{})

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, _ node.Node) error {

		_, err = m.continueLifecycleAction(lifecycleDetail)
		if err != nil {
			return fmt.Errorf("continuing ASG termination lifecycle: %w", err)
		}
		log.Info().Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).Str("instanceID", lifecycleDetail.EC2InstanceID).Msg("Completed ASG Lifecycle Hook")

		close(stopHeartbeatCh)
		return m.deleteMessage(message)
	}

	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		nthConfig := n.GetNthConfig()
		// If only HeartbeatInterval is set, HeartbeatUntil will default to 172800.
		if nthConfig.HeartbeatInterval != -1 && nthConfig.HeartbeatUntil != -1 {
			go m.checkHeartbeatTimeout(nthConfig.HeartbeatInterval, lifecycleDetail)
			go m.SendHeartbeats(nthConfig.HeartbeatInterval, nthConfig.HeartbeatUntil, lifecycleDetail, stopHeartbeatCh)
		}

		err := n.TaintASGLifecycleTermination(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Err(err).Msgf("unable to taint node with taint %s:%s", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID)
		}
		return nil
	}

	return &interruptionEvent, nil
}

// Compare the heartbeatInterval with the heartbeat timeout and warn if (heartbeatInterval >= heartbeat timeout)
func (m SQSMonitor) checkHeartbeatTimeout(heartbeatInterval int, lifecycleDetail *LifecycleDetail) {
	input := &autoscaling.DescribeLifecycleHooksInput{
		AutoScalingGroupName: aws.String(lifecycleDetail.AutoScalingGroupName),
		LifecycleHookNames:   []*string{aws.String(lifecycleDetail.LifecycleHookName)},
	}

	lifecyclehook, err := m.ASG.DescribeLifecycleHooks(input)
	if err != nil {
		log.Err(err).Msg("failed to describe lifecycle hook")
		return
	}

	if len(lifecyclehook.LifecycleHooks) == 0 {
		log.Warn().
			Str("asgName", lifecycleDetail.AutoScalingGroupName).
			Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).
			Msg("Tried to check heartbeat timeout, but no lifecycle hook found from ASG")
		return
	}

	heartbeatTimeout := int(*lifecyclehook.LifecycleHooks[0].HeartbeatTimeout)

	if heartbeatInterval >= heartbeatTimeout {
		log.Warn().Msgf(
			"Heartbeat interval (%d seconds) is equal to or greater than "+
				"the heartbeat timeout (%d seconds) for the lifecycle hook %s attached to ASG %s. "+
				"The node would likely be terminated before the heartbeat is sent",
			heartbeatInterval,
			heartbeatTimeout,
			*lifecyclehook.LifecycleHooks[0].LifecycleHookName,
			*lifecyclehook.LifecycleHooks[0].AutoScalingGroupName,
		)
	}
}

// Issue lifecycle heartbeats to reset the heartbeat timeout timer in ASG
func (m SQSMonitor) SendHeartbeats(heartbeatInterval int, heartbeatUntil int, lifecycleDetail *LifecycleDetail, stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
	defer ticker.Stop()
	timeout := time.After(time.Duration(heartbeatUntil) * time.Second)

	for {
		select {
		case <-stopCh:
			log.Info().Str("asgName", lifecycleDetail.AutoScalingGroupName).
				Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).
				Str("lifecycleActionToken", lifecycleDetail.LifecycleActionToken).
				Str("instanceID", lifecycleDetail.EC2InstanceID).
				Msg("Successfully cordoned and drained the node, stopping heartbeat")
			return
		case <-ticker.C:
			err := m.recordLifecycleActionHeartbeat(lifecycleDetail)
			if err != nil {
				log.Err(err).Msg("invalid heartbeat target, stopping heartbeat")
				return
			}
		case <-timeout:
			log.Info().Str("asgName", lifecycleDetail.AutoScalingGroupName).
				Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).
				Str("lifecycleActionToken", lifecycleDetail.LifecycleActionToken).
				Str("instanceID", lifecycleDetail.EC2InstanceID).
				Msg("Heartbeat deadline exceeded, stopping heartbeat")
			return
		}
	}
}

func (m SQSMonitor) recordLifecycleActionHeartbeat(lifecycleDetail *LifecycleDetail) error {
	input := &autoscaling.RecordLifecycleActionHeartbeatInput{
		AutoScalingGroupName: aws.String(lifecycleDetail.AutoScalingGroupName),
		LifecycleHookName:    aws.String(lifecycleDetail.LifecycleHookName),
		LifecycleActionToken: aws.String(lifecycleDetail.LifecycleActionToken),
		InstanceId:           aws.String(lifecycleDetail.EC2InstanceID),
	}

	// Stop the heartbeat if the target is invalid
	_, err := m.ASG.RecordLifecycleActionHeartbeat(input)
	if err != nil {
		var awsErr awserr.Error
		log.Warn().
			Str("asgName", lifecycleDetail.AutoScalingGroupName).
			Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).
			Str("lifecycleActionToken", lifecycleDetail.LifecycleActionToken).
			Str("instanceID", lifecycleDetail.EC2InstanceID).
			Err(err).
			Msg("Failed to send lifecycle heartbeat")
		if errors.As(err, &awsErr) && awsErr.Code() == "ValidationError" {
			return err
		}
		return nil
	}

	log.Info().
		Str("asgName", lifecycleDetail.AutoScalingGroupName).
		Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).
		Str("lifecycleActionToken", lifecycleDetail.LifecycleActionToken).
		Str("instanceID", lifecycleDetail.EC2InstanceID).
		Msg("Successfully sent lifecycle heartbeat")
	return nil
}

func (m SQSMonitor) deleteMessage(message *sqs.Message) error {
	errs := m.deleteMessages([]*sqs.Message{message})
	if errs != nil {
		return errs[0]
	}
	return nil
}

// Continues the lifecycle hook thereby indicating a successful action occurred
func (m SQSMonitor) continueLifecycleAction(lifecycleDetail *LifecycleDetail) (*autoscaling.CompleteLifecycleActionOutput, error) {
	return m.completeLifecycleAction(&autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  &lifecycleDetail.AutoScalingGroupName,
		LifecycleActionResult: aws.String("CONTINUE"),
		LifecycleHookName:     &lifecycleDetail.LifecycleHookName,
		LifecycleActionToken:  &lifecycleDetail.LifecycleActionToken,
		InstanceId:            &lifecycleDetail.EC2InstanceID,
	})
}

// Completes the ASG launch lifecycle hook if the new EC2 instance launched by ASG is Ready in the cluster
func (m SQSMonitor) createAsgInstanceLaunchEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}

	if message == nil {
		return nil, fmt.Errorf("message is nil")
	}

	lifecycleDetail := &LifecycleDetail{}
	err := json.Unmarshal(event.Detail, lifecycleDetail)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling message, %s, from ASG launch lifecycle event: %w", *message.MessageId, err)
	}

	if lifecycleDetail.Event == TEST_NOTIFICATION || lifecycleDetail.LifecycleTransition == TEST_NOTIFICATION {
		return nil, skip{fmt.Errorf("message is an ASG test notification")}
	}

	nodeInfo, err := m.getNodeInfo(lifecycleDetail.EC2InstanceID)
	if err != nil {
		return nil, err
	}

	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("asg-lifecycle-term-%x", event.ID),
		Kind:                 monitor.ASGLaunchLifecycleKind,
		Monitor:              SQSMonitorKind,
		AutoScalingGroupName: lifecycleDetail.AutoScalingGroupName,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		InstanceID:           lifecycleDetail.EC2InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		Description:          fmt.Sprintf("ASG Lifecycle Launch event received. Instance was started at %s \n", event.getTime()),
	}

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, _ node.Node) error {
		_, err = m.continueLifecycleAction(lifecycleDetail)
		if err != nil {
			return fmt.Errorf("continuing ASG launch lifecycle: %w", err)
		}
		log.Info().Str("lifecycleHookName", lifecycleDetail.LifecycleHookName).Str("instanceID", lifecycleDetail.EC2InstanceID).Msg("Completed ASG Lifecycle Hook")
		return m.deleteMessage(message)
	}

	return &interruptionEvent, err
}
