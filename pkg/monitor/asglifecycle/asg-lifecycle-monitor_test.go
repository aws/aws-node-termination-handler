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

package asglifecycle_test

import (
	"github.com/aws/aws-node-termination-handler/pkg/monitor/asglifecycle"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

const (
	expFormattedTime = "2020-10-26 15:15:15 +0000 UTC"
	imdsV2TokenPath  = "/latest/api/token"
	nodeName         = "test-node"
)

var asgTargetLifecycleStateResponse = []byte("InService")

func TestMonitor_Success(t *testing.T) {
	requestPath := ec2metadata.ASGTargetLifecycleStatePath

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if imdsV2TokenPath == req.URL.String() {
			rw.WriteHeader(403)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		_, err := rw.Write(asgTargetLifecycleStateResponse)
		h.Ok(t, err)
	}))
	defer server.Close()

	drainChan := make(chan monitor.InterruptionEvent)
	cancelChan := make(chan monitor.InterruptionEvent)
	imds := ec2metadata.New(server.URL, 1)

	go func() {
		result := <-drainChan
		h.Equals(t, monitor.ASGLifecycleKind, result.Kind)
		h.Equals(t, asglifecycle.ASGLifecycleMonitorKind, result.Monitor)
		h.Equals(t, expFormattedTime, result.StartTime.String())
	}()

	asgLifecycleMonitor := asglifecycle.NewASGLifecycleMonitor(imds, drainChan, cancelChan, nodeName)
	err := asgLifecycleMonitor.Monitor()
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

	asgLifecycleMonitor := asglifecycle.NewASGLifecycleMonitor(imds, drainChan, cancelChan, nodeName)
	err := asgLifecycleMonitor.Monitor()
	h.Ok(t, err)
}

func TestMonitor_404Response(t *testing.T) {
	requestPath := ec2metadata.ASGTargetLifecycleStatePath

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

	asgLifecycleMonitor := asglifecycle.NewASGLifecycleMonitor(imds, drainChan, cancelChan, nodeName)
	err := asgLifecycleMonitor.Monitor()
	h.Ok(t, err)
}

func TestMonitor_500Response(t *testing.T) {
	requestPath := ec2metadata.ASGTargetLifecycleStatePath

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

	asgLifecycleMonitor := asglifecycle.NewASGLifecycleMonitor(imds, drainChan, cancelChan, nodeName)
	err := asgLifecycleMonitor.Monitor()
	h.Assert(t, err != nil, "Failed to return error when 500 response")
}
