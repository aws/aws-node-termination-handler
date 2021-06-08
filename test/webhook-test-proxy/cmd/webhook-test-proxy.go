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
	"log"
	"net/http"
	"os"
)

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
	// support webhook test
	if req.Method == http.MethodPost {
		res.WriteHeader(http.StatusOK)
		return
	}
	res.WriteHeader(http.StatusBadRequest)
}

func main() {
	log.Println("The webhook-test-proxy started on port ", getListenAddress())
	// start server
	http.HandleFunc("/", handleRequest)
	if err := http.ListenAndServe(getListenAddress(), nil); err != nil {
		panic(err)
	}
}
