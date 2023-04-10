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

package ec2metadata

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func TestRetry(t *testing.T) {
	var numRetries int = 3
	var errorMsg string = "Request failed"
	var requestCount int

	request := func() (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(bytes.NewBufferString(`OK`)),
			Header:     make(http.Header),
		}, errors.New(errorMsg)
	}

	resp, err := retry(numRetries, time.Microsecond, request)
	h.Assert(t, err != nil, "Should have gotten a \"Request failed\" error")
	defer resp.Body.Close()

	h.Equals(t, errorMsg, err.Error())
	h.Equals(t, numRetries, requestCount)
}

func TestGetV2Token(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.Header.Get(tokenTTLHeader), strconv.Itoa(tokenTTL))
		h.Equals(t, req.URL.String(), tokenRefreshPath)
		rw.Header().Set(tokenTTLHeader, "100")
		_, err := rw.Write([]byte(`token`))
		h.Ok(t, err)
	}))
	defer server.Close()
	imds := New(server.URL, 1)

	token, ttl, err := imds.getV2Token()
	h.Ok(t, err)
	h.Equals(t, "token", token)
	h.Equals(t, 100, ttl)
}

func TestGetV2TokenBadURL(t *testing.T) {
	imds := New(string([]byte{0x7f}), 1)
	_, _, err := imds.getV2Token()
	h.Assert(t, err != nil, "Should error on invalid metadata URL")
}

func TestGetV2TokenBadTTLHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.Header.Get(tokenTTLHeader), strconv.Itoa(tokenTTL))
		h.Equals(t, req.URL.String(), tokenRefreshPath)
		rw.Header().Set(tokenTTLHeader, "badttl")
		_, err := rw.Write([]byte(`token`))
		h.Ok(t, err)
	}))
	defer server.Close()
	imds := New(server.URL, 1)

	_, _, err := imds.getV2Token()
	h.Assert(t, err != nil, "Non-int TTL should have caused an error")
}
