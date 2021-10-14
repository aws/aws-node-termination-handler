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
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/rs/zerolog/log"
)

/* Example AWS Health Scheduled Change EC2 Event:
{
  "version": "0",
  "id": "7fb65329-1628-4cf3-a740-95fg457h1402",
  "detail-type": "AWS Health Event",
  "source": "aws.health",
  "account": "account id",
  "time": "2016-06-05T06:27:57Z",
  "region": "us-east-1",
  "resources": ["i-12345678"],
  "detail": {
    "eventArn": "arn:aws:health:region::event/id",
    "service": "EC2",
    "eventTypeCode": "AWS_EC2_DEDICATED_HOST_NETWORK_MAINTENANCE_SCHEDULED",
    "eventTypeCategory": "scheduledChange",
    "startTime": "Sat, 05 Jun 2016 15:10:09 GMT",
    "eventDescription": [{
      "language": "en_US",
      "latestDescription": "A description of the event will be provided here"
    }],
    "affectedEntities": [{
      "entityValue": "i-12345678",
      "tags": {
        "stage": "prod",
        "app": "my-app"
      }
    }]
  }
}
*/

// AffectedEntity holds information about an entity that is affected by a Health event
type AffectedEntity struct {
	EntityValue string `json:"entityValue"`
}

// ScheduledEventDetail holds the event details for AWS Health scheduled EC2 change events from Amazon EventBridge
type ScheduledEventDetail struct {
	EventTypeCategory string           `json:"eventTypeCategory"`
	Service           string           `json:"service"`
	AffectedEntities  []AffectedEntity `json:"affectedEntities"`
}

const supportedEventCategoryTypes = "scheduledChange"

func (m SQSMonitor) scheduledEventToInterruptionEvents(event EventBridgeEvent, message *sqs.Message) ([]InterruptionEventWrapper, error) {
	scheduledEventDetail := &ScheduledEventDetail{}

	if err := json.Unmarshal(event.Detail, scheduledEventDetail); err != nil {
		return nil, err
	}

	if scheduledEventDetail.Service != "EC2" {
		return nil, fmt.Errorf("events from Amazon EventBridge for service (%s) are not supported", scheduledEventDetail.Service)
	}

	if !strings.Contains(supportedEventCategoryTypes, scheduledEventDetail.EventTypeCategory) {
		return nil, fmt.Errorf("events from Amazon EventBridge with EventTypeCategory (%s) are not supported", scheduledEventDetail.EventTypeCategory)
	}

	// interruptionEventWrappers := make([]InterruptionEventWrapper, len(event.Resources))
	interruptionEventWrappers := []InterruptionEventWrapper{}

	for _, affectedEntity := range scheduledEventDetail.AffectedEntities {
		nodeName, err := m.retrieveNodeName(affectedEntity.EntityValue)
		if err != nil {
			// interruptionEventWrappers[i] = InterruptionEventWrapper{nil, err}
			interruptionEventWrappers = append(interruptionEventWrappers, InterruptionEventWrapper{nil, err})
			continue
		}
		asgName, _ := m.retrieveAutoScalingGroupName(affectedEntity.EntityValue)
		interruptionEvent := monitor.InterruptionEvent{
			EventID:              fmt.Sprintf("aws-health-maintenance-event-%x", event.ID),
			Kind:                 SQSTerminateKind,
			AutoScalingGroupName: asgName,
			StartTime:            time.Now(),
			NodeName:             nodeName,
			InstanceID:           affectedEntity.EntityValue,
			Description:          fmt.Sprintf("AWS Health maintenance event received. Instance %s will be interrupted at %s \n", affectedEntity.EntityValue, event.getTime()),
		}
		interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
			errs := m.deleteMessages([]*sqs.Message{message})
			if errs != nil {
				return errs[0]
			}
			return nil
		}
		interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
			err := n.TaintScheduledMaintenance(interruptionEvent.NodeName, interruptionEvent.EventID)
			if err != nil {
				log.Err(err).Msgf("Unable to taint node with taint %s:%s", node.ScheduledMaintenanceTaint, interruptionEvent.EventID)
			}
			return nil
		}

		// interruptionEventWrappers[i] = InterruptionEventWrapper{&interruptionEvent, nil}
		interruptionEventWrappers = append(interruptionEventWrappers, InterruptionEventWrapper{&interruptionEvent, nil})

	}

	return interruptionEventWrappers, nil
}
