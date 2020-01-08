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
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/draineventstore"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
)

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	nthConfig := config.ParseCliArgs()
	drainer.InitDrainer(nthConfig)
	drainEventStore := draineventstore.NewStore(&nthConfig)

	log.Println("Kubernetes AWS Node Termination Handler has started successfully!")
	drainChan := make(chan drainevent.DrainEvent)
	defer close(drainChan)
	monitoringFns := []func(chan<- drainevent.DrainEvent, config.Config){
		draineventstore.MonitorForSpotITNEvents,
	}
	for _, fn := range monitoringFns {
		go fn(drainChan, nthConfig)
	}

	go watchForDrainEvents(drainChan, drainEventStore, nthConfig)
	log.Println("Started watching for drain events")

	for range time.NewTicker(1 * time.Second).C {
		select {
		case _, ok := <-signalChan:
			// Exit drain loop if a SIGTERM is received
			if ok {
				break
			}
			// Exit drain loop if signal channel is closed
			break
		default:
			drainIfNecessary(drainEventStore, nthConfig)
		}
	}
	log.Printf("AWS Node Termination Handler is shutting down")
}

func watchForDrainEvents(drainChan <-chan drainevent.DrainEvent, drainEventStore *draineventstore.Store, nthConfig config.Config) {
	for {
		drainEvent := <-drainChan
		log.Printf("Got drain event from channel %+v\n", drainEvent)
		drainEventStore.AddDrainEvent(&drainEvent)
	}
}

func drainIfNecessary(drainEventStore *draineventstore.Store, nthConfig config.Config) {
	if event, ok := drainEventStore.GetActiveEvent(); ok {
		drainer.Drain(nthConfig)
		drainEventStore.MarkAllAsDrained()
		log.Printf("Node %q successfully drained.\n", nthConfig.NodeName)
		if nthConfig.WebhookURL != "" {
			webhook.Post(event, nthConfig)
		}
	}
}
