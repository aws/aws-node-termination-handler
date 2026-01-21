// Copyright 2020 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package asglifecycle

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
)

// ASGLifecycleMonitorKind is a const to define this monitor kind
const ASGLifecycleMonitorKind = "ASG_LIFECYCLE_MONITOR"

// ASGLifecycleMonitor is a struct definition which facilitates monitoring of ASG target lifecycle state from IMDS
type ASGLifecycleMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	NodeName         string
}

// NewASGLifecycleMonitor creates an instance of a ASG lifecycle IMDS monitor
func NewASGLifecycleMonitor(imds *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, cancelChan chan<- monitor.InterruptionEvent, nodeName string) ASGLifecycleMonitor {
	return ASGLifecycleMonitor{
		IMDS:             imds,
		InterruptionChan: interruptionChan,
		CancelChan:       cancelChan,
		NodeName:         nodeName,
	}
}

// Monitor continuously monitors metadata for ASG target lifecycle state and sends interruption events to the passed in channel
func (m ASGLifecycleMonitor) Monitor() error {
	interruptionEvent, err := m.checkForASGTargetLifecycleStateNotice()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == monitor.ASGLifecycleKind {
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

// Kind denotes the kind of monitor
func (m ASGLifecycleMonitor) Kind() string {
	return ASGLifecycleMonitorKind
}

// checkForASGTargetLifecycleStateNotice Checks EC2 instance metadata for a asg lifecycle termination notice
func (m ASGLifecycleMonitor) checkForASGTargetLifecycleStateNotice() (*monitor.InterruptionEvent, error) {
	state, err := m.IMDS.GetASGTargetLifecycleState()
	if err != nil {
		return nil, fmt.Errorf("there was a problem checking for ASG target lifecycle state: %w", err)
	}
	if state != "Terminated" {
		// if the state is not "Terminated", we can skip. State can also be empty (no hook configured).
		return nil, nil
	}

	nodeName := m.NodeName
	// there is no time in the response, we just set time to the latest check
	interruptionTime := time.Now()

	// There's no EventID returned, so we'll create it using a hash to prevent duplicates.
	hash := sha256.New()
	if _, err = fmt.Fprintf(hash, "%s:%s", state, interruptionTime); err != nil {
		return nil, fmt.Errorf("there was a problem creating an event ID from the event: %w", err)
	}

	return &monitor.InterruptionEvent{
		EventID:      fmt.Sprintf("target-lifecycle-state-terminated-%x", hash.Sum(nil)),
		Kind:         monitor.ASGLifecycleKind,
		Monitor:      ASGLifecycleMonitorKind,
		StartTime:    interruptionTime,
		NodeName:     nodeName,
		Description:  "AST target lifecycle state received. Instance will be terminated\n",
		PreDrainTask: setInterruptionTaint,
	}, nil
}

func setInterruptionTaint(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
	err := n.TaintASGLifecycleTermination(interruptionEvent.NodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("unable to taint node with taint %s:%s: %w", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID, err)
	}

	return nil
}
