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

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	awsAccountId                     = "123456789012"
	source                           = "aws.ec2"
	detail                           = "EC2 Spot Instance Interruption Warning"
	region                           = "us-east-1"
	uuid                             = "58f98edf-5234-4373-89a2-fea575e5eb34"
	instanceId                       = "i-1234567890abcdef0"
	instanceAction                   = "terminate"
	metadataIp                       = "http://169.254.169.254"
	interruptionNoticeDelayConfigKey = "INTERRUPTION_NOTICE_DELAY"
	interruptionNoticeDelayDefault   = "0"
	scheduledActionDateFormat        = "02 Jan 2006 15:04:05 GMT"
	spotInstanceActionFlag           = "ENABLE_SPOT_ITN"
	spotInstanceActionPath           = "/latest/meta-data/spot/instance-action"
	scheduledMaintenanceEventFlag    = "ENABLE_SCHEDULED_MAINTENANCE_EVENTS"
	scheduledMaintenanceEventPath    = "/latest/meta-data/events/maintenance/scheduled"
	scheduledEventStatusConfigKey    = "SCHEDULED_EVENT_STATUS"
	scheduledEventStatusDefault      = "active"
	imdsV2TokenPath                  = "/latest/api/token"
	imdsV2ConfigKey                  = "ENABLE_IMDS_V2"
	imdsV2Token                      = "token"
	tokenTTLHeader                   = "X-aws-ec2-metadata-token-ttl-seconds"
	tokenRequestHeader               = "X-aws-ec2-metadata-token"
)

var startTime int64 = time.Now().Unix()

// ScheduledEventDetail metadata structure for json parsing
type ScheduledEventDetail struct {
	NotBefore   string `json:"NotBefore"`
	Code        string `json:"Code"`
	Description string `json:"Description"`
	EventId     string `json:"EventId"`
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

// Get env var or default
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func isPathEnabled(pathFlag string) bool {
	envVar := getEnv(pathFlag, "")
	if envVar == "" {
		log.Printf("Environment variable \"%s\" is not set, defaulting path to true.\n", pathFlag)
		return true
	}
	enabled, err := strconv.ParseBool(envVar)
	if err != nil {
		log.Printf("Environment variable \"%s\" is not a valid boolean. Treating value as false\n", pathFlag)
		return false
	}
	return enabled
}

// Get the port to listen on
func getListenAddress() string {
	port := getEnv("PORT", "1338")
	return ":" + port
}

func handleRequest(res http.ResponseWriter, req *http.Request) {
	log.Println("GOT REQUEST: ", req.URL.Path)
	requestTime := time.Now().Unix()
	interruptionDelayEnvStr := getEnv(interruptionNoticeDelayConfigKey, interruptionNoticeDelayDefault)
	interruptionDelay, err := strconv.Atoi(interruptionDelayEnvStr)
	if err != nil {
		log.Printf("Could not convert env var %s=%s to integer. Using default instead: %s\n", interruptionNoticeDelayConfigKey, interruptionDelayEnvStr, interruptionNoticeDelayDefault)
		interruptionDelay, _ = strconv.Atoi(interruptionNoticeDelayDefault)
	}
	interruptionDelayRemaining := int64(interruptionDelay) - (requestTime - startTime)
	isV2Enabled, _ := strconv.ParseBool(getEnv(imdsV2ConfigKey, "false"))
	if isV2Enabled {
		log.Println("IMDSv2 is ENABLED! This means v1 API will not work.")
		res.Header().Add(tokenTTLHeader, "1000")
	} else {
		log.Println("IMDSv2 is NOT enabled!")
	}

	if req.URL.Path == imdsV2TokenPath && isV2Enabled {
		if req.Method != http.MethodPut {
			res.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		log.Println("Received IMDSv2 token")
		res.Write([]byte(imdsV2Token))
		return
	}

	if interruptionDelayRemaining > 0 {
		log.Printf("Interruption Notice Delay (%ds  will expire in %ds) has not been reached yet", interruptionDelay, interruptionDelayRemaining)
		res.WriteHeader(404)
		return
	} else if req.URL.Path == spotInstanceActionPath {
		log.Println("Handling Spot Instance Action Path")
		if isV2Enabled {
			if !isTokenValid(req) {
				res.WriteHeader(http.StatusForbidden)
				return
			}
		}
		if !isPathEnabled(spotInstanceActionFlag) {
			http.Error(res, "ec2-metadata-test-proxy feature not enabled", http.StatusNotFound)
			return
		}
		timePlus2Min := time.Now().Local().Add(time.Minute * time.Duration(2)).Format(time.RFC3339)
		arn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", region, awsAccountId, instanceId)
		instanceAction := InstanceAction{
			Version:    "0",
			Id:         uuid,
			DetailType: detail,
			Source:     source,
			Account:    awsAccountId,
			Time:       timePlus2Min,
			Region:     region,
			Resources:  []string{arn},
			Detail:     InstanceActionDetail{instanceId, instanceAction}}
		js, err := json.Marshal(instanceAction)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Header().Set("Content-Type", "application/json")
		res.Write(js)
		return
	} else if req.URL.Path == scheduledMaintenanceEventPath {
		log.Println("Handling Scheduled Maintenance Events Path")
		if isV2Enabled {
			if !isTokenValid(req) {
				res.WriteHeader(http.StatusForbidden)
				return
			}
		}
		if !isPathEnabled(scheduledMaintenanceEventFlag) {
			http.Error(res, "ec2-metadata-test-proxy feature not enabled", http.StatusNotFound)
			return
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
		timePlus2Min := time.Now().Local().Add(time.Minute * 2).Format(scheduledActionDateFormat)
		timePlus4Min := time.Now().Local().Add(time.Minute * 4).Format(scheduledActionDateFormat)
		scheduledEvent := ScheduledEventDetail{
			NotBefore:   timePlus2Min,
			Code:        "system-reboot",
			Description: "scheduled reboot",
			EventId:     "instance-event-0d59937288b749b32",
			NotAfter:    timePlus4Min,
			State:       getEnv(scheduledEventStatusConfigKey, scheduledEventStatusDefault),
		}
		js, err := json.Marshal([]ScheduledEventDetail{scheduledEvent})
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Header().Set("Content-Type", "application/json")
		res.Write(js)
		return
	}
	res.Header().Set("Content-Type", "application/json")
	res.Write([]byte("{}"))
	return
}

func isTokenValid(req *http.Request) bool {
	token := req.Header.Get("X-aws-ec2-metadata-token")
	log.Printf("Token evaluation: header(%s) -> %s", token, imdsV2Token)
	if token != imdsV2Token {
		return false
	}
	return true
}

func main() {
	log.Println("The ec2-metadata-test-proxy started on port ", getListenAddress())
	// start server
	http.HandleFunc("/", handleRequest)
	if err := http.ListenAndServe(getListenAddress(), nil); err != nil {
		panic(err)
	}
}
