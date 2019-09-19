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
	AWS_ACCOUNT_ID  = "123456789012"
	SOURCE          = "aws.ec2"
	DETAIL          = "EC2 Spot Instance Interruption Warning"
	REGION          = "us-east-1"
	UUID            = "58f98edf-5234-4373-89a2-fea575e5eb34"
	INSTANCE_ID     = "i-1234567890abcdef0"
	INSTANCE_ACTION = "terminate"
	META_DATA_IP    = "http://169.254.169.254"
)

type InstanceActionDetail struct {
	InstanceId     string `json:"instance-id"`
	InstanceAction string `json:"instance-action"`
}

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
		time_plus_2_min := time.Now().Local().Add(time.Minute * time.Duration(2)).Format(time.RFC3339)
		arn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", REGION, AWS_ACCOUNT_ID, INSTANCE_ID)
		instance_action := InstanceAction{
			Version:    "0",
			Id:         UUID,
			DetailType: DETAIL,
			Source:     SOURCE,
			Account:    AWS_ACCOUNT_ID,
			Time:       time_plus_2_min,
			Region:     REGION,
			Resources:  []string{arn},
			Detail:     InstanceActionDetail{INSTANCE_ID, INSTANCE_ACTION}}
		js, err := json.Marshal(instance_action)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Header().Set("Content-Type", "application/json")
		res.Write(js)
		return
	}
	meta_data_url, _ := url.Parse(META_DATA_IP)
	httputil.NewSingleHostReverseProxy(meta_data_url).ServeHTTP(res, req)
}

func main() {
	log.Println("The ec2 meta-data-proxy started on port ", getListenAddress())
	// start server
	http.HandleFunc("/", handleRequest)
	if err := http.ListenAndServe(getListenAddress(), nil); err != nil {
		panic(err)
	}
}
