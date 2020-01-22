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

func TestRequestMetadata(t *testing.T) {
	var requestPath string = "/some/path"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.URL.String(), requestPath)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	// Use Client & URL from our local test server
	resp, err := ec2metadata.RequestMetadata(server.URL, requestPath)
	defer resp.Body.Close()

	h.Ok(t, err)
	h.Equals(t, http.StatusOK, resp.StatusCode)

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error("Unable to parse response.")
	}

	h.Equals(t, []byte("OK"), responseData)
}
