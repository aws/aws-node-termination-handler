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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// SpotInstanceActionPath is the context path to spot/instance-action within IMDS
	SpotInstanceActionPath = "/latest/meta-data/spot/instance-action"
	// ScheduledEventPath is the context path to events/maintenance/scheduled within IMDS
	ScheduledEventPath = "/latest/meta-data/events/maintenance/scheduled"
	// InstanceIDPath path to instance id
	InstanceIDPath = "/latest/meta-data/instance-id"
	// InstanceTypePath path to instance type
	InstanceTypePath = "/latest/meta-data/instance-type"
	// PublicHostnamePath path to public hostname
	PublicHostnamePath = "/latest/meta-data/public-hostname"
	// PublicIPPath path to public ip
	PublicIPPath = "/latest/meta-data/public-ipv4"
	// LocalHostnamePath path to local hostname
	LocalHostnamePath = "/latest/meta-data/local-hostname"
	// LocalIPPath path to local ip
	LocalIPPath = "/latest/meta-data/local-ipv4"
	// AZPlacementPath path to availability zone placement
	AZPlacementPath = "/latest/meta-data/placement/availability-zone"

	// IMDSv2 token related constants
	tokenRefreshPath        = "/latest/api/token"
	tokenTTLHeader          = "X-aws-ec2-metadata-token-ttl-seconds"
	tokenRequestHeader      = "X-aws-ec2-metadata-token"
	tokenTTL                = 3600 // 1 hour
	secondsBeforeTTLRefresh = 15
	tokenRetryAttempts      = 2
)

// Service is used to query the EC2 instance metadata service v1 and v2
type Service struct {
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

// InstanceAction metadata structure for json parsing
type InstanceAction struct {
	Action string `json:"action"`
	Time   string `json:"time"`
}

// NodeMetadata contains information that applies to every drain event
type NodeMetadata struct {
	InstanceID       string
	InstanceType     string
	PublicHostname   string
	PublicIP         string
	LocalHostname    string
	LocalIP          string
	AvailabilityZone string
}

// New constructs an instance of the Service client
func New(metadataURL string, tries int) *Service {
	return &Service{
		metadataURL: metadataURL,
		tries:       tries,
		httpClient: http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:    10,
				IdleConnTimeout: 30 * time.Second,
			},
		},
	}
}

// GetScheduledMaintenanceEvents retrieves EC2 scheduled maintenance events from imds
func (e *Service) GetScheduledMaintenanceEvents() ([]ScheduledEventDetail, error) {
	resp, err := e.Request(ScheduledEventPath)
	if resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return nil, fmt.Errorf("Metadata request received http status code: %d", resp.StatusCode)
	}
	if err != nil {
		return nil, fmt.Errorf("Unable to parse metadata response: %w", err)
	}
	defer resp.Body.Close()
	var scheduledEvents []ScheduledEventDetail
	err = json.NewDecoder(resp.Body).Decode(&scheduledEvents)
	if err != nil {
		return nil, fmt.Errorf("Could not decode json retrieved from imds: %w", err)
	}
	return scheduledEvents, nil
}

// GetSpotITNEvent retrieves EC2 spot interruption events from imds
func (e *Service) GetSpotITNEvent() (instanceAction *InstanceAction, err error) {
	resp, err := e.Request(SpotInstanceActionPath)
	// 404s are normal when querying for the 'latest/meta-data/spot' path
	if resp != nil && resp.StatusCode == 404 {
		return nil, nil
	} else if resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return nil, fmt.Errorf("Metadata request received http status code: %d", resp.StatusCode)
	}
	if err != nil {
		return nil, fmt.Errorf("Unable to parse metadata response: %w", err)
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&instanceAction)
	if err != nil {
		return nil, fmt.Errorf("Could not decode instance action response: %w", err)
	}
	return instanceAction, nil
}

// GetMetadataInfo generic function for retrieving ec2 metadata
func (e *Service) GetMetadataInfo(path string) (info string, err error) {
	resp, err := e.Request(path)
	if resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return "", fmt.Errorf("Metadata request received http status code: %d", resp.StatusCode)
	}
	if err != nil {
		return "", fmt.Errorf("Unable to parse metadata response: %w", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Unable to parse http response: %w", err)
	}
	return string(body), nil
}

// Request sends an http request to IMDSv1 or v2 at the specified path
// It is up to the caller to handle http status codes on the response
// An error will only be returned if the request is unable to be made
func (e *Service) Request(contextPath string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, e.metadataURL+contextPath, nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to construct an http get request to IDMS for %s: %w", e.metadataURL+contextPath, err)
	}
	var resp *http.Response
	for i := 0; i < tokenRetryAttempts; i++ {
		if e.v2Token == "" || e.tokenTTL <= secondsBeforeTTLRefresh {
			e.Lock()
			token, ttl, err := e.getV2Token()
			if err != nil {
				e.v2Token = ""
				e.tokenTTL = -1
				log.Log().Err(err).Msg("Unable to retrieve an IMDSv2 token, continuing with IMDSv1")
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
		resp, err = retry(e.tries, 2*time.Second, httpReq)
		if err != nil {
			return nil, fmt.Errorf("Unable to get a response from IMDS: %w", err)
		}
		if resp != nil && resp.StatusCode == 401 {
			e.v2Token = ""
			e.tokenTTL = 0
		} else {
			break
		}
	}
	ttl, err := ttlHeaderToInt(resp)
	if err == nil {
		e.Lock()
		e.tokenTTL = ttl
		e.Unlock()
	}
	return resp, nil
}

func (e *Service) getV2Token() (string, int, error) {
	req, err := http.NewRequest(http.MethodPut, e.metadataURL+tokenRefreshPath, nil)
	if err != nil {
		return "", -1, fmt.Errorf("Unable to construct http put request to retrieve imdsv2 token: %w", err)
	}
	req.Header.Add(tokenTTLHeader, strconv.Itoa(tokenTTL))
	httpReq := func() (*http.Response, error) {
		return e.httpClient.Do(req)
	}
	log.Log().Msg("Trying to get token from IMDSv2")
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
	log.Log().Msg("Got token from IMDSv2")
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

			log.Log().Msgf("Request failed. Attempts remaining: %d", attempts)
			log.Log().Msgf("Sleep for %s seconds", sleep)
			time.Sleep(sleep)
			return retry(attempts, 2*sleep, httpReq)
		}
	}

	return resp, err
}

// GetNodeMetadata attempts to gather additional ec2 instance information from the metadata service
func (e *Service) GetNodeMetadata() NodeMetadata {
	var metadata NodeMetadata
	metadata.InstanceID, _ = e.GetMetadataInfo(InstanceIDPath)
	metadata.InstanceType, _ = e.GetMetadataInfo(InstanceTypePath)
	metadata.PublicHostname, _ = e.GetMetadataInfo(PublicHostnamePath)
	metadata.PublicIP, _ = e.GetMetadataInfo(PublicIPPath)
	metadata.LocalHostname, _ = e.GetMetadataInfo(LocalHostnamePath)
	metadata.LocalIP, _ = e.GetMetadataInfo(LocalIPPath)
	metadata.AvailabilityZone, _ = e.GetMetadataInfo(AZPlacementPath)

	log.Log().Interface("metadata", metadata).Msg("Startup Metadata Retrieved")

	return metadata
}
