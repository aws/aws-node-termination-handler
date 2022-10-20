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
		Description:          fmt.Sprintf("ASG Lifecycle Termination event received. Instance will be interrupted at %s \n", event.getTime()),
	}

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, _ node.Node) error {
		_, err := m.completeLifecycleAction(&autoscaling.CompleteLifecycleActionInput{
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
		log.Info().Msgf("Completed ASG Lifecycle Hook (%s) for instance %s",
			lifecycleDetail.LifecycleHookName,
			lifecycleDetail.EC2InstanceID)
		errs := m.deleteMessages([]*sqs.Message{message})
		if errs != nil {
			return errs[0]
		}
		return nil
	}

	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintASGLifecycleTermination(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Err(err).Msgf("Unable to taint node with taint %s:%s", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID)
		}
		return nil
	}

	return &interruptionEvent, nil
}
