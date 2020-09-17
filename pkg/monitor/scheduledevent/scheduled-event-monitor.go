// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package scheduledevent

import (
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/rs/zerolog/log"
)

const (
	// ScheduledEventKind is a const to define a scheduled event kind of interruption event
	ScheduledEventKind           = "SCHEDULED_EVENT"
	scheduledEventStateCompleted = "completed"
	scheduledEventStateCanceled  = "canceled"
	scheduledEventDateFormat     = "2 Jan 2006 15:04:05 GMT"
	instanceStopCode             = "instance-stop"
	systemRebootCode             = "system-reboot"
	instanceRebootCode           = "instance-reboot"
	instanceRetirementCode       = "instance-retirement"
)

// ScheduledEventMonitor is a struct definition that knows how to process scheduled events from IMDS
type ScheduledEventMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	NodeName         string
}

// NewScheduledEventMonitor creates an instance of a scheduled event monitor
func NewScheduledEventMonitor(imds *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, cancelChan chan<- monitor.InterruptionEvent, nodeName string) ScheduledEventMonitor {
	return ScheduledEventMonitor{
		IMDS:             imds,
		InterruptionChan: interruptionChan,
		CancelChan:       cancelChan,
		NodeName:         nodeName,
	}
}

// Monitor continuously monitors metadata for scheduled events and sends interruption events to the passed in channel
func (m ScheduledEventMonitor) Monitor() error {
	interruptionEvents, err := m.checkForScheduledEvents()
	if err != nil {
		return err
	}
	for _, interruptionEvent := range interruptionEvents {
		if isStateCanceledOrCompleted(interruptionEvent.State) {
			m.CancelChan <- interruptionEvent
		} else {
			m.InterruptionChan <- interruptionEvent
		}
	}
	return nil
}

// Kind denotes the kind of event that is processed
func (m ScheduledEventMonitor) Kind() string {
	return ScheduledEventKind
}

// checkForScheduledEvents Checks EC2 instance metadata for a scheduled event requiring a node drain
func (m ScheduledEventMonitor) checkForScheduledEvents() ([]monitor.InterruptionEvent, error) {
	scheduledEvents, err := m.IMDS.GetScheduledMaintenanceEvents()
	if err != nil {
		return nil, fmt.Errorf("Unable to parse metadata response: %w", err)
	}

	events := make([]monitor.InterruptionEvent, 0)
	for _, scheduledEvent := range scheduledEvents {
		var preDrainFunc monitor.DrainTask
		if isRestartEvent(scheduledEvent.Code) && !isStateCanceledOrCompleted(scheduledEvent.State) {
			preDrainFunc = uncordonAfterRebootPreDrain
		}
		notBefore, err := time.Parse(scheduledEventDateFormat, scheduledEvent.NotBefore)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse scheduled event start time: %w", err)
		}
		notAfter, err := time.Parse(scheduledEventDateFormat, scheduledEvent.NotAfter)
		if err != nil {
			notAfter = notBefore
			log.Log().Err(err).Msg("Unable to parse scheduled event end time, continuing")
		}
		events = append(events, monitor.InterruptionEvent{
			EventID:      scheduledEvent.EventID,
			Kind:         ScheduledEventKind,
			Description:  fmt.Sprintf("%s will occur between %s and %s because %s\n", scheduledEvent.Code, scheduledEvent.NotBefore, scheduledEvent.NotAfter, scheduledEvent.Description),
			State:        scheduledEvent.State,
			NodeName:     m.NodeName,
			StartTime:    notBefore,
			EndTime:      notAfter,
			PreDrainTask: preDrainFunc,
		})
	}
	return events, nil
}

func uncordonAfterRebootPreDrain(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
	nodeName := interruptionEvent.NodeName
	err := n.MarkWithEventID(nodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("Unable to mark node with event ID: %w", err)
	}

	err = n.TaintScheduledMaintenance(nodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("Unable to taint node with taint %s:%s: %w", node.ScheduledMaintenanceTaint, interruptionEvent.EventID, err)
	}

	// if the node is already marked as unschedulable, then don't do anything
	unschedulable, err := n.IsUnschedulable(nodeName)
	if err == nil && unschedulable {
		log.Log().Msg("Node is already marked unschedulable, not taking any action to add uncordon label.")
		return nil
	} else if err != nil {
		return fmt.Errorf("Encountered an error while checking if the node is unschedulable. Not setting an uncordon label: %w", err)
	}
	err = n.MarkForUncordonAfterReboot(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to mark the node for uncordon: %w", err)
	}
	log.Log().Msg("Successfully applied uncordon after reboot action label to node.")
	return nil
}

func isStateCanceledOrCompleted(state string) bool {
	return state == scheduledEventStateCanceled ||
		state == scheduledEventStateCompleted
}

func isRestartEvent(maintenanceCode string) bool {
	return maintenanceCode == instanceStopCode ||
		maintenanceCode == instanceRetirementCode ||
		maintenanceCode == instanceRebootCode ||
		maintenanceCode == systemRebootCode
}
