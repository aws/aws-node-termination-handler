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
	"io/ioutil"
	"net/http"
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
			Body:       ioutil.NopCloser(bytes.NewBufferString(`OK`)),
			Header:     make(http.Header),
		}, errors.New(errorMsg)
	}

	resp, err := retry(numRetries, time.Microsecond, request)
	defer resp.Body.Close()

	h.Equals(t, errorMsg, err.Error())
	h.Equals(t, numRetries, requestCount)
}
