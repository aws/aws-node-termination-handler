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
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/service/sqs"
)

/* Example EC2 State Change Event:
{
  "version": "0",
  "id": "7bf73129-1428-4cd3-a780-95db273d1602",
  "detail-type": "EC2 Instance State-change Notification",
  "source": "aws.ec2",
  "account": "123456789012",
  "time": "2015-11-11T21:29:54Z",
  "region": "us-east-1",
  "resources": [
    "arn:aws:ec2:us-east-1:123456789012:instance/i-abcd1111"
  ],
  "detail": {
    "instance-id": "i-abcd1111",
    "state": "pending"
  }
}
*/

// EC2StateChangeDetail holds the event details for EC2 state change events from Amazon EventBridge
type EC2StateChangeDetail struct {
	InstanceID string `json:"instance-id"`
	State      string `json:"state"`
}

const instanceStatesToDrain = "stopping,stopped,shutting-down,terminated"

func (m SQSMonitor) ec2StateChangeToInterruptionEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	ec2StateChangeDetail := &EC2StateChangeDetail{}
	err := json.Unmarshal(event.Detail, ec2StateChangeDetail)
	if err != nil {
		return nil, err
	}

	if !strings.Contains(instanceStatesToDrain, strings.ToLower(ec2StateChangeDetail.State)) {
		return nil, nil
	}

	nodeInfo, err := m.getNodeInfo(ec2StateChangeDetail.InstanceID)
	if err != nil {
		return nil, err
	}
	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("ec2-state-change-event-%x", event.ID),
		Kind:                 monitor.StateChangeKind,
		Monitor:              SQSMonitorKind,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		AutoScalingGroupName: nodeInfo.AsgName,
		InstanceID:           ec2StateChangeDetail.InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		InstanceType:         nodeInfo.InstanceType,
		Description:          fmt.Sprintf("EC2 State Change event received. Instance %s went into %s at %s \n", ec2StateChangeDetail.InstanceID, ec2StateChangeDetail.State, event.getTime()),
	}

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		errs := m.deleteMessages([]*sqs.Message{message})
		if errs != nil {
			return errs[0]
		}
		return nil
	}
	return &interruptionEvent, nil
}
