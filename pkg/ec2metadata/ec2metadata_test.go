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
// permissions and limitations under the License

package ec2metadata_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func TestRequestV1(t *testing.T) {
	var requestPath string = "/some/path"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(401)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	resp, err := imds.Request(requestPath)
	h.Ok(t, err)
	defer resp.Body.Close()
	h.Equals(t, http.StatusOK, resp.StatusCode)

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error("Unable to parse response.")
	}

	h.Equals(t, []byte("OK"), responseData)
}

func TestRequestV2(t *testing.T) {
	var requestPath string = "/some/path"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	resp, err := imds.Request(requestPath)
	h.Ok(t, err)
	defer resp.Body.Close()
	h.Equals(t, http.StatusOK, resp.StatusCode)

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error("Unable to parse response.")
	}

	h.Equals(t, []byte("OK"), responseData)
}

func TestRequestFailure(t *testing.T) {
	var requestPath string = "/some/path"
	imds := ec2metadata.New("notadomain", 1)

	_, err := imds.Request(requestPath)
	h.Assert(t, err != nil, "imds request failed")
}

func TestRequest500(t *testing.T) {
	var requestPath string = "/some/path"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(401)
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		rw.WriteHeader(500)
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	resp, err := imds.Request(requestPath)
	h.Ok(t, err)
	h.Equals(t, 500, resp.StatusCode)
}

func TestRequest401(t *testing.T) {
	var requestPath string = "/some/path"

	tokenGenerationCounter := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.URL.String(), requestPath)
		if tokenGenerationCounter < 1 {
			rw.WriteHeader(401)
			tokenGenerationCounter++
		} else {
			rw.WriteHeader(200)
		}

	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	resp, err := imds.Request(requestPath)
	h.Ok(t, err)
	h.Equals(t, 200, resp.StatusCode)
	h.Equals(t, 1, tokenGenerationCounter)
}

func TestRequestConstructFail(t *testing.T) {
	imds := ec2metadata.New("test", 0)

	_, err := imds.Request(string([]byte{0x7f}))
	h.Assert(t, err != nil, "imds request failed")
}

func TestGetSpotITNEventSuccess(t *testing.T) {
	const (
		time           = "2020-02-07T14:55:55Z"
		instanceAction = "terminate"
	)
	var requestPath string = "/latest/meta-data/spot/instance-action"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(fmt.Sprintf(`{
			"action": "%s",
			"time": "%s"
		}`, instanceAction, time)))
	}))
	defer server.Close()

	expectedStruct := &ec2metadata.InstanceAction{
		Action: instanceAction,
		Time:   time,
	}

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	spotITN, err := imds.GetSpotITNEvent()
	h.Ok(t, err)
	h.Equals(t, expectedStruct, spotITN)
}

func TestGetSpotITNEvent404Success(t *testing.T) {
	var requestPath string = "/latest/meta-data/spot/instance-action"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.WriteHeader(404)
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	spotITN, err := imds.GetSpotITNEvent()
	h.Ok(t, err)
	h.Assert(t, spotITN == nil, "SpotITN Event should be nil")
}

func TestGetSpotITNEventBadJSON(t *testing.T) {
	var requestPath string = "/latest/meta-data/spot/instance-action"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`{"action": false}`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	_, err := imds.GetSpotITNEvent()
	h.Assert(t, err != nil, "JSON returned should not be in the correct format")
}

func TestGetSpotITNEvent500Failure(t *testing.T) {
	var requestPath string = "/latest/meta-data/spot/instance-action"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.WriteHeader(500)
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	_, err := imds.GetSpotITNEvent()
	h.Assert(t, err != nil, "error expected on non-200 or non-404 status code")
}

func TestGetSpotITNEventRequestFailure(t *testing.T) {
	// Use URL from our local test server
	imds := ec2metadata.New("/some-path-that-will-error", 1)

	_, err := imds.GetSpotITNEvent()
	h.Assert(t, err != nil, "error expected because no server should be running")
}

func TestGetScheduledMaintenanceEventsSuccess(t *testing.T) {
	const (
		notBefore   = "21 Jan 2019 09:00:43 GMT"
		notAfter    = "21 Jan 2019 09:17:23 GMT"
		code        = "system-reboot"
		description = "scheduled reboot"
		eventId     = "instance-event-0d59937288b749b32"
		state       = "active"
	)
	var requestPath string = "/latest/meta-data/events/maintenance/scheduled"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(fmt.Sprintf(`[
			{
			  "NotBefore" : "%s",
			  "Code" : "%s",
			  "Description" : "%s",
			  "EventId" : "%s",
			  "NotAfter" : "%s",
			  "State" : "%s"
			}
		  ]`, notBefore, code, description, eventId, notAfter, state)))
	}))
	defer server.Close()

	expectedStructs := []ec2metadata.ScheduledEventDetail{
		{
			NotBefore:   notBefore,
			Code:        code,
			Description: description,
			EventID:     eventId,
			NotAfter:    notAfter,
			State:       state,
		},
	}

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	maintenanceEvents, err := imds.GetScheduledMaintenanceEvents()
	h.Ok(t, err)
	h.Equals(t, expectedStructs, maintenanceEvents)
}

func TestGetScheduledMaintenanceEvents500Failure(t *testing.T) {
	var requestPath string = "/latest/meta-data/events/maintenance/scheduled"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.WriteHeader(500)
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	_, err := imds.GetScheduledMaintenanceEvents()
	h.Assert(t, err != nil, "error expected on non-200 status code")
}

func TestGetScheduledMaintenanceEventsBadJSON(t *testing.T) {
	var requestPath string = "/latest/meta-data/events/maintenance/scheduled"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`{"notBefore": false}`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	_, err := imds.GetScheduledMaintenanceEvents()
	h.Assert(t, err != nil, "JSON returned should not be in the correct format")
}

func TestGetScheduledMaintenanceEventsRequestFailure(t *testing.T) {
	// Use URL from our local test server
	imds := ec2metadata.New("/some-path-that-will-error", 1)

	_, err := imds.GetScheduledMaintenanceEvents()
	h.Assert(t, err != nil, "error expected because no server should be running")
}

func TestGetMetadataServiceRequest404(t *testing.T) {
	var requestPath string = "/latest/meta-data/instance-type"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.WriteHeader(404)
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	_, err := imds.GetMetadataInfo(requestPath)

	h.Assert(t, err != nil, "Expected error to be nil but it was not")
}

func TestGetMetadataServiceRequestFailure(t *testing.T) {
	// Use URL from our local test server
	imds := ec2metadata.New("/some-path-that-will-error", 1)

	_, err := imds.GetMetadataInfo("/latest/meta-data/instance-type")
	h.Assert(t, err != nil, "Error expected because no server should be running")
}

func TestGetMetadataServiceSuccess(t *testing.T) {
	var requestPath string = "/latest/meta-data/instance-type"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		h.Equals(t, req.Header.Get("X-aws-ec2-metadata-token"), "token")
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`x1.32xlarge`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)

	resp, err := imds.GetMetadataInfo(requestPath)

	h.Ok(t, err)
	h.Equals(t, `x1.32xlarge`, resp)
}

func TestGetNodeMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Add("X-aws-ec2-metadata-token-ttl-seconds", "100")
		if req.URL.String() == "/latest/api/token" {
			rw.WriteHeader(200)
			rw.Write([]byte(`token`))
			return
		}
		rw.Write([]byte(`metadata`))
	}))
	defer server.Close()

	// Use URL from our local test server
	imds := ec2metadata.New(server.URL, 1)
	nodeMetadata := imds.GetNodeMetadata()

	h.Assert(t, nodeMetadata.InstanceID == `metadata`, `Missing required NodeMetadata field InstanceID`)
	h.Assert(t, nodeMetadata.InstanceType == `metadata`, `Missing required NodeMetadata field InstanceType`)
	h.Assert(t, nodeMetadata.LocalHostname == `metadata`, `Missing required NodeMetadata field LocalHostname`)
	h.Assert(t, nodeMetadata.LocalIP == `metadata`, `Missing required NodeMetadata field LocalIP`)
	h.Assert(t, nodeMetadata.PublicHostname == `metadata`, `Missing required NodeMetadata field PublicHostname`)
	h.Assert(t, nodeMetadata.PublicIP == `metadata`, `Missing required NodeMetadata field PublicIP`)
	h.Assert(t, nodeMetadata.AvailabilityZone == `metadata`, `Missing required NodeMetadata field AvailabilityZone`)
}
