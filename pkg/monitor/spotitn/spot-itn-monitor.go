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

package spotitn

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
)

// SpotITNMonitorKind is a const to define this monitor kind
const SpotITNMonitorKind = "SPOT_ITN_MONITOR"

// SpotInterruptionMonitor is a struct definition which facilitates monitoring of spot ITNs from IMDS
type SpotInterruptionMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	NodeName         string
}

// NewSpotInterruptionMonitor creates an instance of a spot ITN IMDS monitor
func NewSpotInterruptionMonitor(imds *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, cancelChan chan<- monitor.InterruptionEvent, nodeName string) SpotInterruptionMonitor {
	return SpotInterruptionMonitor{
		IMDS:             imds,
		InterruptionChan: interruptionChan,
		CancelChan:       cancelChan,
		NodeName:         nodeName,
	}
}

// Monitor continuously monitors metadata for spot ITNs and sends interruption events to the passed in channel
func (m SpotInterruptionMonitor) Monitor() error {
	interruptionEvent, err := m.checkForSpotInterruptionNotice()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == monitor.SpotITNKind {
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

// Kind denotes the kind of monitor
func (m SpotInterruptionMonitor) Kind() string {
	return SpotITNMonitorKind
}

// checkForSpotInterruptionNotice Checks EC2 instance metadata for a spot interruption termination notice
func (m SpotInterruptionMonitor) checkForSpotInterruptionNotice() (*monitor.InterruptionEvent, error) {
	instanceAction, err := m.IMDS.GetSpotITNEvent()
	if instanceAction == nil && err == nil {
		// if there are no spot itns and no errors
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("There was a problem checking for spot ITNs: %w", err)
	}
	nodeName := m.NodeName
	interruptionTime, err := time.Parse(time.RFC3339, instanceAction.Time)
	if err != nil {
		return nil, fmt.Errorf("Could not parse time from spot interruption notice metadata json: %w", err)
	}

	// There's no EventID returned so we'll create it using a hash to prevent duplicates.
	hash := sha256.New()
	_, err = hash.Write([]byte(fmt.Sprintf("%v", instanceAction)))
	if err != nil {
		return nil, fmt.Errorf("There was a problem creating an event ID from the event: %w", err)
	}

	return &monitor.InterruptionEvent{
		EventID:      fmt.Sprintf("spot-itn-%x", hash.Sum(nil)),
		Kind:         monitor.SpotITNKind,
		Monitor:      SpotITNMonitorKind,
		StartTime:    interruptionTime,
		NodeName:     nodeName,
		Description:  fmt.Sprintf("Spot ITN received. Instance will be interrupted at %s \n", instanceAction.Time),
		PreDrainTask: setInterruptionTaint,
	}, nil
}

func setInterruptionTaint(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
	err := n.TaintSpotItn(interruptionEvent.NodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("Unable to taint node with taint %s:%s: %w", node.SpotInterruptionTaint, interruptionEvent.EventID, err)
	}

	return nil
}
