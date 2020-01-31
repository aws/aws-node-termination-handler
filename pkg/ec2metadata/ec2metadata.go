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
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// SpotInstanceActionPath is the context path to spot/instance-action within IMDS
	SpotInstanceActionPath = "/latest/meta-data/spot/instance-action"
	// ScheduledEventPath is the context path to events/maintenance/scheduled within IMDS
	ScheduledEventPath = "/latest/meta-data/events/maintenance/scheduled"

	// IMDSv2 token related constants
	tokenRefreshPath        = "/latest/api/token"
	tokenTTLHeader          = "X-aws-ec2-metadata-token-ttl-seconds"
	tokenRequestHeader      = "X-aws-ec2-metadata-token"
	tokenTTL                = 3600 // 1 hour
	secondsBeforeTTLRefresh = 15
)

// EC2MetadataService is used to query the EC2 instance metadata service v1 and v2
type EC2MetadataService struct {
	httpClient  http.Client
	tries       int
	metadataURL string
	v2Token     string
	tokenTTL    int
	sync.RWMutex
}

// [
//   {
//     "NotBefore" : "21 Jan 2019 09:00:43 GMT",
//     "Code" : "system-reboot",
//     "Description" : "scheduled reboot",
//     "EventId" : "instance-event-0d59937288b749b32",
//     "NotAfter" : "21 Jan 2019 09:17:23 GMT",
//     "State" : "active"
//   }
// ]

// ScheduledEventDetail metadata structure for json parsing
type ScheduledEventDetail struct {
	NotBefore   string `json:"NotBefore"`
	Code        string `json:"Code"`
	Description string `json:"Description"`
	EventID     string `json:"EventId"`
	NotAfter    string `json:"NotAfter"`
	State       string `json:"State"`
}

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

// New constructs an instance of the EC2MetadataService client
func New(metadataURL string, tries int) *EC2MetadataService {
	return &EC2MetadataService{
		metadataURL: metadataURL,
		tries:       tries,
		httpClient:  http.Client{},
	}
}

// Request sends an http request to IMDSv1 or v2 at the specified path
func (e *EC2MetadataService) Request(contextPath string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, e.metadataURL+contextPath, nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to construct an http get request to IDMS for %s: %w", e.metadataURL+contextPath, err)
	}
	if e.v2Token == "" || e.tokenTTL <= secondsBeforeTTLRefresh {
		e.Lock()
		token, ttl, err := e.getV2Token()
		if err != nil {
			e.v2Token = ""
			e.tokenTTL = -1
			log.Printf("Unable to retrieve an IMDSv2 token, continuing with IMDSv1: %v", err)
		} else {
			e.v2Token = token
			e.tokenTTL = ttl
		}
		e.Unlock()
	}
	if e.v2Token != "" {
		req.Header.Add(tokenRequestHeader, e.v2Token)
	}
	httpReq := func() (*http.Response, error) {
		return e.httpClient.Do(req)
	}
	resp, err := retry(e.tries, 2*time.Second, httpReq)
	if err != nil {
		return nil, fmt.Errorf("Unable to get a response from IMDS: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Unable to request metadata, received http status code: %d", resp.StatusCode)
	}
	ttl, err := ttlHeaderToInt(resp)
	if err == nil {
		e.Lock()
		e.tokenTTL = ttl
		e.Unlock()
	}
	return resp, nil
}

func (e *EC2MetadataService) getV2Token() (string, int, error) {
	req, err := http.NewRequest(http.MethodPut, e.metadataURL+tokenRefreshPath, nil)
	if err != nil {
		return "", -1, fmt.Errorf("Unable to construct http put request to retrieve imdsv2 token: %w", err)
	}
	req.Header.Add(tokenTTLHeader, strconv.Itoa(tokenTTL))
	httpReq := func() (*http.Response, error) {
		return e.httpClient.Do(req)
	}
	log.Println("Trying to get token from IMDSv2")
	resp, err := retry(1, 2*time.Second, httpReq)
	if err != nil {
		return "", -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", -1, fmt.Errorf("Received an http status code %d", resp.StatusCode)
	}
	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", -1, fmt.Errorf("Unable to read token response from IMDSv2: %w", err)
	}
	ttl, err := ttlHeaderToInt(resp)
	if err != nil {
		return "", -1, fmt.Errorf("IMDS v2 Token TTL header not sent in response: %w", err)
	}
	log.Println("Got token from IMDSv2")
	return string(token), ttl, nil
}

func ttlHeaderToInt(resp *http.Response) (int, error) {
	ttl := resp.Header.Get(tokenTTLHeader)
	if ttl == "" {
		return -1, fmt.Errorf("No token TTL header found")
	}
	ttlInt, err := strconv.Atoi(ttl)
	if err != nil {
		return -1, err
	}
	return ttlInt, nil
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
	}

	return resp, err
}
