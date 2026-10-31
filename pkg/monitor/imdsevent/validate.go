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

package imdsevent

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/asglifecycle"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/rebalancerecommendation"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
)

// A node writes its report itself, so the controller treats every field of it as untrusted: it drops an event with a
// field it doesn't expect, and generates the description of an event the same way the IMDS monitor on the node does,
// except for the free text of scheduled events.

// scheduledEventDateFormat is the date format of the description of scheduled events
const scheduledEventDateFormat = "2 Jan 2006 15:04:05 GMT"

// eventKind is what the controller expects of the events of a kind
type eventKind struct {
	monitor     string
	eventID     *regexp.Regexp
	description func(e Event) string
}

// the IMDS monitors name their events with a SHA-256 hash, except scheduled events, which have the ID IMDS gave them
var eventKinds = map[string]eventKind{
	monitor.SpotITNKind: {
		monitor: spotitn.SpotITNMonitorKind,
		eventID: regexp.MustCompile(`^spot-itn-[0-9a-f]{64}$`),
		description: func(e Event) string {
			return fmt.Sprintf("Spot ITN received. Instance will be interrupted at %s \n", e.StartTime.UTC().Format(time.RFC3339))
		},
	},
	monitor.RebalanceRecommendationKind: {
		monitor: rebalancerecommendation.RebalanceRecommendationMonitorKind,
		eventID: regexp.MustCompile(`^rebalance-recommendation-[0-9a-f]{64}$`),
		description: func(e Event) string {
			return fmt.Sprintf("Rebalance recommendation received. Instance will be cordoned at %s \n", e.StartTime.UTC().Format(time.RFC3339))
		},
	},
	monitor.ASGLifecycleKind: {
		monitor: asglifecycle.ASGLifecycleMonitorKind,
		eventID: regexp.MustCompile(`^target-lifecycle-state-terminated-[0-9a-f]{64}$`),
		description: func(Event) string {
			return "AST target lifecycle state received. Instance will be terminated\n"
		},
	},
	monitor.ScheduledEventKind: {
		monitor: scheduledevent.ScheduledEventMonitorKind,
		eventID: regexp.MustCompile(`^instance-event-[0-9a-f]{1,32}$`),
		// without the description IMDS gives the event, which the controller can't check
		description: func(e Event) string {
			return fmt.Sprintf("%s will occur between %s and %s\n", e.Code, e.NotBefore.UTC().Format(scheduledEventDateFormat), e.EndTime.UTC().Format(scheduledEventDateFormat))
		},
	},
}

// scheduledEventCodes are the codes of scheduled events, and whether the instance restarts for them
var scheduledEventCodes = map[string]bool{
	"instance-reboot":     true,
	"system-reboot":       true,
	"instance-stop":       true,
	"instance-retirement": true,
	"system-maintenance":  false,
}

// scheduledEventStates are the states of scheduled events
var scheduledEventStates = map[string]bool{"": true, "active": true, "canceled": true, "completed": true}

// instanceLifeCycles are the values of the instance-life-cycle IMDS category
var instanceLifeCycles = map[string]bool{"on-demand": true, "spot": true, "scheduled": true, "capacity-block": true}

var accountID = regexp.MustCompile(`^[0-9]{12}$`)

// validEvent checks a reported event, and returns it with the monitor of its kind and the description the controller
// generates for it. The errors name the field, never its value.
func validEvent(e Event) (Event, string, error) {
	kind, ok := eventKinds[e.Kind]
	if !ok {
		return Event{}, "", errors.New("unknown event kind")
	}
	if !kind.eventID.MatchString(e.EventID) {
		return Event{}, "", fmt.Errorf("invalid event ID for kind %s", e.Kind)
	}
	e.Monitor = kind.monitor
	if e.Kind == monitor.ScheduledEventKind {
		restarts, ok := scheduledEventCodes[e.Code]
		if !ok {
			return Event{}, "", errors.New("unknown scheduled event code")
		}
		if !scheduledEventStates[e.State] {
			return Event{}, "", errors.New("unknown scheduled event state")
		}
		e.UncordonAfterReboot = e.UncordonAfterReboot && restarts
	} else {
		// only scheduled events have these
		e.State, e.Code, e.NotBefore, e.UncordonAfterReboot = "", "", time.Time{}, false
	}
	return e, kind.description(e), nil
}

// validMetadata returns the reported metadata with the fields that don't have the form IMDS gives them emptied
func validMetadata(m ReportedMetadata) ReportedMetadata {
	if !accountID.MatchString(m.AccountID) {
		m.AccountID = ""
	}
	if !instanceLifeCycles[m.InstanceLifeCycle] {
		m.InstanceLifeCycle = ""
	}
	return m
}
