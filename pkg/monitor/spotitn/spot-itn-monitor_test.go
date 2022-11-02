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

package spotitn_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

const (
	startTime        = "2017-09-18T08:22:00Z"
	expFormattedTime = "2017-09-18 08:22:00 +0000 UTC"
	imdsV2TokenPath  = "/latest/api/token"
	nodeName         = "test-node"
)

var instanceActionResponse = []byte(`{
	"action": "terminate",
	"time":"` + startTime + `"
}`)

func TestMonitor_Success(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		_, err := rw.Write(instanceActionResponse)
		h.Ok(t, err)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)

	go func() {
		result := <-drainChan
		h.Equals(t, monitor.SpotITNKind, result.Kind)
		h.Equals(t, spotitn.SpotITNMonitorKind, result.Monitor)
		h.Equals(t, expFormattedTime, result.StartTime.String())
		h.Assert(t, strings.Contains(result.Description, startTime),
			"Expected description to contain: "+startTime+" but is actually: "+result.Description)
	}()

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Ok(t, err)
}

func TestMonitor_MetadataParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)
	nodeName := "test-node"

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Assert(t, err != nil, "Failed to return error metadata parse fails")
}

func TestMonitor_404Response(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		http.Error(rw, "error", http.StatusNotFound)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)
	nodeName := "test-node"

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Ok(t, err)
}

func TestMonitor_500Response(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		http.Error(rw, "error", http.StatusInternalServerError)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)
	nodeName := "test-node"

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Assert(t, err != nil, "Failed to return error when 500 response")
}

func TestMonitor_InstanceActionDecodeFailure(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		_, err := rw.Write([]byte{0x7f})
		h.Ok(t, err)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)
	nodeName := "test-node"

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Assert(t, err != nil, "Failed to return error when failed to decode instance action")
}

func TestMonitor_TimeParseFailure(t *testing.T) {
	var requestPath string = ec2metadata.SpotInstanceActionPath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		_, err := rw.Write([]byte(`{"time": ""}`))
		h.Ok(t, err)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)
	nodeName := "test-node"

	spotITNMonitor := spotitn.NewSpotInterruptionMonitor(imds, drainChan, cancelChan, nodeName)
	err := spotITNMonitor.Monitor()
	h.Assert(t, err != nil, "Failed to return error when failed to parse time")
}
