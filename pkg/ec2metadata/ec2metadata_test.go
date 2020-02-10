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

func TestRequestConstructFail(t *testing.T) {
	imds := ec2metadata.New("test", 0)

	_, err := imds.Request(string([]byte{0x7f}))
	h.Assert(t, err != nil, "imds request failed")
}

func TestGetSpotITNEventSuccess(t *testing.T) {
	const (
		version        = "0"
		id             = "12345678-1234-1234-1234-123456789012"
		detailType     = "EC2 Spot Instance Interruption Warning"
		source         = "aws.ec2"
		account        = "123456789012"
		time           = "2020-02-07T14:55:55Z"
		region         = "us-east-2"
		resource       = "arn:aws:ec2:us-east-2:123456789012:instance/i-1234567890abcdef0"
		instanceAction = "terminate"
		instanceId     = "i-1234567890abcdef0"
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
			"version": "%s",
			"id": "%s",
			"detail-type": "%s",
			"source": "%s",
			"account": "%s",
			"time": "%s",
			"region": "%s",
			"resources": ["%s"],
			"detail": {
				"instance-id": "%s",
				"instance-action": "%s"
			}
		}`, version, id, detailType, source, account, time, region, resource, instanceId, instanceAction)))
	}))
	defer server.Close()

	expectedStruct := &ec2metadata.InstanceAction{
		Version:    version,
		Id:         id,
		DetailType: detailType,
		Source:     source,
		Account:    account,
		Time:       time,
		Region:     region,
		Resources:  []string{resource},
		Detail: ec2metadata.InstanceActionDetail{
			InstanceId:     instanceId,
			InstanceAction: instanceAction,
		},
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
		rw.Write([]byte(`{"version": false}`))
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
		ec2metadata.ScheduledEventDetail{
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
