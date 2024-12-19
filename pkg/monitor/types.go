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

package monitor

import (
	"strings"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/node"
)

const (
	// SpotITNKind is a const to define a Spot ITN kind of interruption event
	SpotITNKind = "SPOT_ITN"
	// ScheduledEventKind is a const to define a scheduled event kind of interruption event
	ScheduledEventKind = "SCHEDULED_EVENT"
	// RebalanceRecommendationKind is a const to define a Rebalance Recommendation kind of interruption event
	RebalanceRecommendationKind = "REBALANCE_RECOMMENDATION"
	// StateChangeKind is a const to define an EC2 State Change kind of interruption event
	StateChangeKind = "STATE_CHANGE"
	// ASGLifecycleKind is a const to define an ASG Lifecycle kind of interruption event
	ASGLifecycleKind = "ASG_LIFECYCLE"
	// ASGLifecycleKind is a const to define an ASG Launch Lifecycle kind of interruption event
	ASGLaunchLifecycleKind = "ASG_LAUNCH_LIFECYCLE"
	// SQSTerminateKind is a const to define an SQS termination kind of interruption event
	SQSTerminateKind = "SQS_TERMINATE"
)

// DrainTask defines a task to be run when draining a node
type DrainTask func(InterruptionEvent, node.Node) error

// InterruptionEvent gives more context of the interruption event
type InterruptionEvent struct {
	EventID              string
	Kind                 string
	Monitor              string
	Description          string
	State                string
	AutoScalingGroupName string
	NodeName             string
	NodeLabels           map[string]string
	Pods                 []string
	InstanceID           string
	ProviderID           string
	InstanceType         string
	IsManaged            bool
	StartTime            time.Time
	EndTime              time.Time
	NodeProcessed        bool
	InProgress           bool
	PreDrainTask         DrainTask `json:"-"`
	PostDrainTask        DrainTask `json:"-"`
}

// TimeUntilEvent returns the duration until the event start time
func (e *InterruptionEvent) TimeUntilEvent() time.Duration {
	return time.Until(e.StartTime)
}

// IsRebalanceRecommendation returns true if the interruption event is a rebalance recommendation
func (e *InterruptionEvent) IsRebalanceRecommendation() bool {
	return strings.Contains(e.EventID, "rebalance-recommendation")
}

// Monitor is an interface which can be implemented for various sources of interruption events
type Monitor interface {
	Monitor() error
	Kind() string
}
