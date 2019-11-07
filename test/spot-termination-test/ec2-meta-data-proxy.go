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
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

const (
	awsAccountId   = "123456789012"
	source         = "aws.ec2"
	detail         = "EC2 Spot Instance Interruption Warning"
	region         = "us-east-1"
	uuid           = "58f98edf-5234-4373-89a2-fea575e5eb34"
	instanceId     = "i-1234567890abcdef0"
	instanceAction = "terminate"
	metadataIp     = "http://169.254.169.254"
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

// Get env var or default
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Get the port to listen on
func getListenAddress() string {
	port := getEnv("PORT", "1338")
	return ":" + port
}

func handleRequest(res http.ResponseWriter, req *http.Request) {
	log.Println("GOT REQUEST: ", req.URL.Path)
	if req.URL.Path == "/latest/meta-data/spot/instance-action" {
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
	}
	metadataUrl, _ := url.Parse(metadataIp)
	httputil.NewSingleHostReverseProxy(metadataUrl).ServeHTTP(res, req)
}

func main() {
	log.Println("The ec2 meta-data-proxy started on port ", getListenAddress())
	// start server
	http.HandleFunc("/", handleRequest)
	if err := http.ListenAndServe(getListenAddress(), nil); err != nil {
		panic(err)
	}
}
