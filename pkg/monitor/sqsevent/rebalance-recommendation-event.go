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

/* Example Rebalance Recommendation Event:
{
	"version": "0",
	"id": "5d5555d5-dd55-5555-5555-5555dd55d55d",
	"detail-type": "EC2 Instance Rebalance Recommendation",
	"source": "aws.ec2",
	"account": "123456789012",
	"time": "2020-10-26T14:14:14Z",
	"region": "us-east-1",
	"resources": [
	  "arn:aws:ec2:us-east-1b:instance/i-0b662ef9931388ba0"
	],
	"detail": {
	  "instance-id": "i-0b662ef9931388ba0"
	}
}
*/

// RebalanceRecommendationDetail holds the event details for rebalance recommendation events from Amazon EventBridge
type RebalanceRecommendationDetail struct {
	InstanceID string `json:"instance-id"`
}

func (m SQSMonitor) rebalanceRecommendationToInterruptionEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	rebalanceRecDetail := &RebalanceRecommendationDetail{}
	err := json.Unmarshal(event.Detail, rebalanceRecDetail)
	if err != nil {
		return nil, err
	}

	nodeInfo, err := m.getNodeInfo(rebalanceRecDetail.InstanceID)
	if err != nil {
		return nil, err
	}
	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("rebalance-recommendation-event-%x", event.ID),
		Kind:                 monitor.RebalanceRecommendationKind,
		Monitor:              SQSMonitorKind,
		AutoScalingGroupName: nodeInfo.AsgName,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		InstanceID:           nodeInfo.InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		InstanceType:         nodeInfo.InstanceType,
		Description:          fmt.Sprintf("Rebalance recommendation event received. Instance %s will be cordoned at %s \n", rebalanceRecDetail.InstanceID, event.getTime()),
	}
	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		errs := m.deleteMessages([]*sqs.Message{message})
		if errs != nil {
			return errs[0]
		}
		return nil
	}
	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintRebalanceRecommendation(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Err(err).Msgf("Unable to taint node with taint %s:%s", node.RebalanceRecommendationTaint, interruptionEvent.EventID)
		}
		return nil
	}
	return &interruptionEvent, nil
}
