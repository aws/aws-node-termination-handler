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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

var eventId = "12345678-1234-1234-1234-123456789012"
var startTime = "2017-09-18T08:22:00Z"
var expFormattedTime = "2017-09-18 08:22:00 +0000 UTC"
var instanceId = 12345
var instanceAction = "INSTANCE_ACTION"

var instanceActionResponse = []byte(`{
    "version": "0",
    "id": "` + eventId + `",
    "detail-type": "EC2 Spot Instance Interruption Warning",
    "source": "aws.ec2",
    "account": "123456789012",
    "time": "` + startTime + `",
    "region": "us-east-2",
    "resources": ["arn:aws:ec2:us-east-2:123456789012:instance/i-1234567890abcdef0"],
    "detail": {
        "instance-id": "` + strconv.Itoa(instanceId) + `",
        "instance-action": "` + instanceAction + `"
    }
}`)

func TestMonitorForSpotITNEventsSuccess(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write(instanceActionResponse)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: server.URL,
	}

	go func() {
		result := <-drainChan
		h.Equals(t, eventId, result.EventID)
		h.Equals(t, drainevent.SpotITNKind, result.Kind)
		h.Equals(t, expFormattedTime, result.StartTime.String())
		h.Assert(t, true, "Drain event description does not contain instance id",
			strings.Contains(result.Description, strconv.Itoa(instanceId)))
		h.Assert(t, true, "Drain event description does not contain instance action",
			strings.Contains(result.Description, instanceAction))
		h.Assert(t, true, "Drain event description does not contain instance time",
			strings.Contains(result.Description, startTime))
		fmt.Println(result)
	}()

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	h.Ok(t, err)
}

func TestMonitorForSpotITNEventsMetadataParseFailure(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: "Bad url",
	}

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	h.Assert(t, true, "Failed to return error metadata parse fails", err != nil)
}

func TestMonitorForSpotITNEvents404Response(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		http.Error(rw, "error", http.StatusNotFound)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: server.URL,
	}

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	h.Assert(t, true, "Failed to return error when 404 response", err != nil)
}

func TestMonitorForSpotITNEvents500Response(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		http.Error(rw, "error", http.StatusInternalServerError)
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: server.URL,
	}

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	h.Assert(t, true, "Failed to return error when 500 response", err != nil)
}

func TestMonitorForSpotITNEventsInstanceActionDecodeFailure(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte{0x7f})
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: server.URL,
	}

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	h.Assert(t, true, "Failed to return error when failed to decode instance action", err != nil)
}

func TestMonitorForSpotITNEventsTimeParseFailure(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`{"time": ""}`))
	}))
	defer server.Close()

	drainChan := make(chan drainevent.DrainEvent)
	cancelChan := make(chan drainevent.DrainEvent)
	nthConfig := config.Config{
		MetadataURL: server.URL,
	}

	err := drainevent.MonitorForSpotITNEvents(drainChan, cancelChan, nthConfig)
	fmt.Printf(err.Error())
	h.Assert(t, true, "Failed to return error when failed to parse time", err != nil)
}
