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

package drainevent_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

const (
	scheduledEventId              = "instance-event-0d59937288b749b32"
	scheduledEventState           = "active"
	scheduledEventCode            = "system-reboot"
	scheduledEventStartTime       = "21 Jan 2019 09:00:43 GMT"
	expScheduledEventStartTimeFmt = "2019-01-21 09:00:43 +0000 UTC"
	scheduledEventEndTime         = "21 Jan 2019 09:17:23 GMT"
	expScheduledEventEndTimeFmt   = "2019-01-21 09:17:23 +0000 UTC"
	scheduledEventDescription     = "scheduled reboot"
	imdsV2TokenPath               = "/latest/api/token"
)

var scheduledEventResponse = []byte(`[{
	"NotBefore": "` + scheduledEventStartTime + `",
	"Code": "` + scheduledEventCode + `",
	"Description": "` + scheduledEventDescription + `",
	"EventId": "` + scheduledEventId + `",
	"NotAfter": "` + scheduledEventEndTime + `",
	"State": "` + scheduledEventState + `"
}]`)

func TestMonitorForScheduledEventsSuccess(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write(scheduledEventResponse)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New(server.URL, 1)

	go func() {
		result := <-drainChan
		h.Equals(t, scheduledEventId, result.EventID)
		h.Equals(t, drainevent.ScheduledEventKind, result.Kind)
		h.Equals(t, scheduledEventState, result.State)
		h.Equals(t, expScheduledEventStartTimeFmt, result.StartTime.String())
		h.Equals(t, expScheduledEventEndTimeFmt, result.EndTime.String())

		h.Assert(t, true, "Drain event description does not contain scheduled event code",
			strings.Contains(result.Description, scheduledEventCode))
		h.Assert(t, true, "Drain event description does not contain start time",
			strings.Contains(result.Description, expScheduledEventStartTimeFmt))
		h.Assert(t, true, "Drain event description does not contain end time",
			strings.Contains(result.Description, expScheduledEventEndTimeFmt))
		h.Assert(t, true, "Drain event description does not contain event description",
			strings.Contains(result.Description, scheduledEventDescription))

	}()

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Ok(t, err)
}

func TestMonitorForScheduledEventsCancelledEvent(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath
	var state = "cancelled"
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`[{
			"NotBefore": "` + scheduledEventStartTime + `",
			"Code": "` + scheduledEventCode + `",
			"Description": "` + scheduledEventDescription + `",
			"EventId": "` + scheduledEventId + `",
			"NotAfter": "` + scheduledEventEndTime + `",
			"State": "` + state + `"
		}]`))
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New(server.URL, 1)

	go func() {
		result := <-cancelChan
		h.Equals(t, scheduledEventId, result.EventID)
		h.Equals(t, drainevent.ScheduledEventKind, result.Kind)
		h.Equals(t, state, result.State)
		h.Equals(t, expScheduledEventStartTimeFmt, result.StartTime.String())
		h.Equals(t, expScheduledEventEndTimeFmt, result.EndTime.String())

		h.Assert(t, true, "Drain event description does not contain scheduled event code",
			strings.Contains(result.Description, scheduledEventCode))
		h.Assert(t, true, "Drain event description does not contain start time",
			strings.Contains(result.Description, expScheduledEventStartTimeFmt))
		h.Assert(t, true, "Drain event description does not contain end time",
			strings.Contains(result.Description, expScheduledEventEndTimeFmt))
		h.Assert(t, true, "Drain event description does not contain event description",
			strings.Contains(result.Description, scheduledEventDescription))

	}()

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Ok(t, err)
}

func TestMonitorForScheduledEventsMetadataParseFailure(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New("bad url", 0)

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Assert(t, true, "Failed to return error when metadata parse fails", err != nil)
}

func TestMonitorForScheduledEvents404Response(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		http.Error(rw, "error", http.StatusNotFound)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New(server.URL, 1)

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Assert(t, true, "Failed to return error when 404 response", err != nil)
}

func TestMonitorForScheduledEventsStartTimeParseFail(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`[{
			"NotBefore": "",
			"Code": "` + scheduledEventCode + `",
			"Description": "` + scheduledEventDescription + `",
			"EventId": "` + scheduledEventId + `",
			"NotAfter": "` + scheduledEventEndTime + `",
			"State": "active"
		}]`))
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New(server.URL, 1)

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Assert(t, true, "Failed to return error when failed to parse start time", err != nil)
}

func TestMonitorForScheduledEventsEndTimeParseFail(t *testing.T) {
	var requestPath string = ec2metadata.ScheduledEventPath
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`[{
			"NotBefore": "` + scheduledEventStartTime + `",
			"Code": "` + scheduledEventCode + `",
			"Description": "` + scheduledEventDescription + `",
			"EventId": "` + scheduledEventId + `",
			"NotAfter": "",
			"State": "active"
		}]`))
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	imds := ec2metadata.New(server.URL, 1)

	err := drainevent.MonitorForScheduledEvents(drainChan, cancelChan, imds)
	h.Assert(t, true, "Failed to return error when failed to parse end time", err != nil)
}
