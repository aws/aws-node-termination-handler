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

	_, err := imds.Request(requestPath)
	h.Assert(t, err != nil, "imds request failed")
}

func TestRequestConstructFail(t *testing.T) {
	// Use URL from our local test server
	imds := ec2metadata.New("test", 0)

	_, err := imds.Request(string([]byte{0x7f}))
	h.Assert(t, err != nil, "imds request failed")
}
