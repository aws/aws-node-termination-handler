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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/drainer"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
)

func main() {
	setupSignalHandler()
	nthConfig := config.ParseCliArgs()
	drainer.InitDrainer(nthConfig)

	log.Println("Kubernetes Spot Node Termination Handler has started successfully!")
	waitForTermination(nthConfig)
	drainer.Drain(nthConfig)
	log.Printf("Node %q successfully drained.\n", nthConfig.NodeName)

	// Sleep to prevent process from restarting.
	// The node should be terminated after configured drain time
	time.Sleep(time.Duration(nthConfig.DrainTimeBeforeTermination) * time.Second)
}

func shouldDrainNode(metadataURL string, drainTimeBeforeTermination int) bool {
	return ec2metadata.CheckForSpotInterruptionNotice(metadataURL, drainTimeBeforeTermination)
}

func waitForTermination(nthConfig config.Config) {
	for range time.Tick(time.Second * 1) {
		if shouldDrainNode(nthConfig.MetadataURL, nthConfig.DrainTimeBeforeTermination) {
			break
		}
	}
}

func setupSignalHandler() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("Ignoring %s", sig)
	}()
}
