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

package monitor_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func TestTimeUntilEvent(t *testing.T) {
	startTime := time.Now().Add(time.Second * 10)
	expected := time.Until(startTime).Round(time.Second)

	event := &monitor.InterruptionEvent{
		StartTime: startTime,
	}

	result := event.TimeUntilEvent()
	h.Equals(t, expected, result.Round(time.Second))
}

func TestIsRebalanceRecommendation_Monitor_Success(t *testing.T) {
	monitorEventId := "rebalance-recommendation-"
	event := &monitor.InterruptionEvent{
		EventID: monitorEventId,
	}

	h.Equals(t, true, event.IsRebalanceRecommendation())
}

func TestIsRebalanceRecommendation_SQS_Success(t *testing.T) {
	sqsEventId := "rebalance-recommendation-event-"
	event := &monitor.InterruptionEvent{
		EventID: sqsEventId,
	}

	h.Equals(t, true, event.IsRebalanceRecommendation())
}

func TestIsRebalanceRecommendation_Failure(t *testing.T) {
	eventId := "reblaance-recommendation"
	event := &monitor.InterruptionEvent{
		EventID: eventId,
	}

	h.Equals(t, false, event.IsRebalanceRecommendation())
}

func TestIsRebalanceRecommendation_Empty_Failure(t *testing.T) {
	event := &monitor.InterruptionEvent{}
	h.Equals(t, false, event.IsRebalanceRecommendation())
}

func TestInterruptionEventJSONOmitsPodDetails(t *testing.T) {
	event := monitor.InterruptionEvent{
		Pods: []string{"agent-1"},
		PodDetails: []node.PodDetail{
			{Name: "agent-1", Annotations: map[string]string{"example.com/secret": "value"}},
		},
	}

	eventJSON, err := json.Marshal(event)
	h.Ok(t, err)
	h.Assert(t, !strings.Contains(string(eventJSON), "PodDetails"), "PodDetails should not be serialized")
	h.Assert(t, !strings.Contains(string(eventJSON), "example.com/secret"), "pod annotations should not be serialized")
}
