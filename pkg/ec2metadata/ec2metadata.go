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

package ec2metadata

import (
	"log"
	"math/rand"
	"net/http"
	"time"
)

const (
	SpotInstanceActionPath = "/latest/meta-data/spot/instance-action"
)

// InstanceActionDetail metadata structure for json parsing
type InstanceActionDetail struct {
	InstanceId     string `json:"instance-id"`
	InstanceAction string `json:"instance-action"`
}

// InstanceAction metadata structure for json parsing
type InstanceAction struct {
	Version    string               `json:"version"`
	Id         string               `json:"id"`
	DetailType string               `json:"detail-type"`
	Source     string               `json:"source"`
	Account    string               `json:"account"`
	Time       string               `json:"time"`
	Region     string               `json:"region"`
	Resources  []string             `json:"resources"`
	Detail     InstanceActionDetail `json:"detail"`
}

// RequestMetadata sends an http request to IMDS at the specified path using the specified URL
func RequestMetadata(metadataURL string, contextPath string) (*http.Response, error) {
	httpReq := func() (*http.Response, error) {
		return http.Get(metadataURL + contextPath)
	}
	return retry(3, 2*time.Second, httpReq)
}

func retry(attempts int, sleep time.Duration, httpReq func() (*http.Response, error)) (*http.Response, error) {
	resp, err := httpReq()
	if err != nil {
		if attempts--; attempts > 0 {
			jitter := time.Duration(rand.Int63n(int64(sleep)))
			sleep = sleep + jitter/2

			log.Printf("Request failed. Attempts remaining: %d\n", attempts)
			log.Printf("Sleep for %s seconds\n", sleep)
			time.Sleep(sleep)
			return retry(attempts, 2*sleep, httpReq)
		}

		log.Fatalln("Error getting response from instance metadata ", err.Error())
	}

	return resp, err
}
