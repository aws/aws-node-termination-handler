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

package rebalancenotice

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
)

const (
	// RebalanceNoticeKind is a const to define a Rebalance Notice kind of event
	RebalanceNoticeKind = "REBALANCE_NOTICE"
)

// RebalanceNoticeMonitor is a struct definition which facilitates monitoring of rebalance notices from IMDS
type RebalanceNoticeMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	CancelChan       chan<- monitor.InterruptionEvent
	NodeName         string
}

// NewRebalanceNoticeMonitor creates an instance of a rebalance notice IMDS monitor
func NewRebalanceNoticeMonitor(imds *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, cancelChan chan<- monitor.InterruptionEvent, nodeName string) RebalanceNoticeMonitor {
	return RebalanceNoticeMonitor{
		IMDS:             imds,
		InterruptionChan: interruptionChan,
		CancelChan:       cancelChan,
		NodeName:         nodeName,
	}
}

// Monitor continuously monitors metadata for rebalance notices and sends interruption events to the passed in channel
func (m RebalanceNoticeMonitor) Monitor() error {
	interruptionEvent, err := m.checkForRebalanceNotice()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == RebalanceNoticeKind {
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

// Kind denotes the kind of event that is processed
func (m RebalanceNoticeMonitor) Kind() string {
	return RebalanceNoticeKind
}

// checkForRebalanceNotice Checks EC2 instance metadata for a rebalance notice
func (m RebalanceNoticeMonitor) checkForRebalanceNotice() (*monitor.InterruptionEvent, error) {
	rebalanceNotice, err := m.IMDS.GetRebalanceNoticeEvent()
	if err != nil {
		return nil, fmt.Errorf("There was a problem checking for rebalance notices: %w", err)
	}
	if rebalanceNotice == nil {
		// if there are no rebalance notices and no errors
		return nil, nil
	}
	nodeName := m.NodeName
	noticeTime, err := time.Parse(time.RFC3339, rebalanceNotice.NoticeTime)
	if err != nil {
		return nil, fmt.Errorf("Could not parse time from rebalance notice metadata json: %w", err)
	}

	// There's no EventID returned so we'll create it using a hash to prevent duplicates.
	hash := sha256.New()
	hash.Write([]byte(fmt.Sprintf("%v", rebalanceNotice)))

	return &monitor.InterruptionEvent{
		EventID:      fmt.Sprintf("rebalance-notice-%x", hash.Sum(nil)),
		Kind:         RebalanceNoticeKind,
		StartTime:    noticeTime,
		NodeName:     nodeName,
		Description:  fmt.Sprintf("Rebalance notice received. Instance will be cordoned at %s \n", rebalanceNotice.NoticeTime),
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
