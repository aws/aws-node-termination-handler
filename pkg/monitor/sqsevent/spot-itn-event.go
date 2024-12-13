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
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/rs/zerolog/log"
)

/* Example Spot ITN Event:
{
	"version": "0",
	"id": "1e5527d7-bb36-4607-3370-4164db56a40e",
	"detail-type": "EC2 Spot Instance Interruption Warning",
	"source": "aws.ec2",
	"account": "<account_number>",
	"time": "1970-01-01T00:00:00Z",
	"region": "us-east-1",
	"resources": [
	  "arn:aws:ec2:us-east-1b:instance/i-0b662ef9931388ba0"
	],
	"detail": {
	  "instance-id": "i-0b662ef9931388ba0",
	  "instance-action": "terminate"
	}
}
*/

// SpotInterruptionDetail holds the event details for spot interruption events from Amazon EventBridge
type SpotInterruptionDetail struct {
	InstanceID     string `json:"instance-id"`
	InstanceAction string `json:"instance-action"`
}

func (m SQSMonitor) spotITNTerminationToInterruptionEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	spotInterruptionDetail := &SpotInterruptionDetail{}
	err := json.Unmarshal(event.Detail, spotInterruptionDetail)
	if err != nil {
		return nil, err
	}

	nodeInfo, err := m.getNodeInfo(spotInterruptionDetail.InstanceID)
	if err != nil {
		return nil, err
	}
	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("spot-itn-event-%x", event.ID),
		Kind:                 monitor.SpotITNKind,
		Monitor:              SQSMonitorKind,
		AutoScalingGroupName: nodeInfo.AsgName,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		InstanceID:           spotInterruptionDetail.InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		InstanceType:         nodeInfo.InstanceType,
		Description:          fmt.Sprintf("Spot Interruption notice for instance %s was sent at %s \n", spotInterruptionDetail.InstanceID, event.getTime()),
	}
	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		errs := m.deleteMessages([]*sqs.Message{message})
		if errs != nil {
			return errs[0]
		}
		return nil
	}
	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintSpotItn(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Err(err).Msgf("Unable to taint node with taint %s:%s", node.SpotInterruptionTaint, interruptionEvent.EventID)
		}
		return nil
	}
	return &interruptionEvent, nil
}
