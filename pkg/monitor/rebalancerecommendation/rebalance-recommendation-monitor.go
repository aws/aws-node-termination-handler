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

package rebalancerecommendation

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
)

// RebalanceRecommentadionMonitorKind is a const to define this monitor kind
const RebalanceRecommendationMonitorKind = "REBALANCE_RECOMMENDATION_MONITOR"

// RebalanceRecommendationMonitor is a struct definition which facilitates monitoring of rebalance recommendations from IMDS
type RebalanceRecommendationMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	NodeName         string
}

// NewRebalanceRecommendationMonitor creates an instance of a rebalance recoomendation IMDS monitor
func NewRebalanceRecommendationMonitor(imds *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, nodeName string) RebalanceRecommendationMonitor {
	return RebalanceRecommendationMonitor{
		IMDS:             imds,
		InterruptionChan: interruptionChan,
		NodeName:         nodeName,
	}
}

// Monitor continuously monitors metadata for rebalance recommendations and sends interruption events to the passed in channel
func (m RebalanceRecommendationMonitor) Monitor() error {
	interruptionEvent, err := m.checkForRebalanceRecommendation()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == monitor.RebalanceRecommendationKind {
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

// Kind denotes the kind of monitor
func (m RebalanceRecommendationMonitor) Kind() string {
	return RebalanceRecommendationMonitorKind
}

// checkForRebalanceRecommendation Checks EC2 instance metadata for a rebalance recommendation
func (m RebalanceRecommendationMonitor) checkForRebalanceRecommendation() (*monitor.InterruptionEvent, error) {
	rebalanceRecommendation, err := m.IMDS.GetRebalanceRecommendationEvent()
	if err != nil {
		return nil, fmt.Errorf("There was a problem checking for rebalance recommendations: %w", err)
	}
	if rebalanceRecommendation == nil {
		// if there are no rebalance recommendations and no errors
		return nil, nil
	}
	nodeName := m.NodeName
	noticeTime, err := time.Parse(time.RFC3339, rebalanceRecommendation.NoticeTime)
	if err != nil {
		return nil, fmt.Errorf("Could not parse time from rebalance recommendation metadata json: %w", err)
	}

	// There's no EventID returned so we'll create it using a hash to prevent duplicates.
	hash := sha256.New()
	_, err = hash.Write([]byte(fmt.Sprintf("%v", rebalanceRecommendation)))
	if err != nil {
		return nil, fmt.Errorf("There was a problem creating an event ID from the event: %w", err)
	}

	return &monitor.InterruptionEvent{
		EventID:      fmt.Sprintf("rebalance-recommendation-%x", hash.Sum(nil)),
		Kind:         monitor.RebalanceRecommendationKind,
		Monitor:      RebalanceRecommendationMonitorKind,
		StartTime:    noticeTime,
		NodeName:     nodeName,
		Description:  fmt.Sprintf("Rebalance recommendation received. Instance will be cordoned at %s \n", rebalanceRecommendation.NoticeTime),
		PreDrainTask: setInterruptionTaint,
	}, nil
}

func setInterruptionTaint(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
	err := n.TaintRebalanceRecommendation(interruptionEvent.NodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("Unable to taint node with taint %s:%s: %w", node.RebalanceRecommendationTaint, interruptionEvent.EventID, err)
	}

	return nil
}
