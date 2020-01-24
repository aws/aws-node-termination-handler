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
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/draineventstore"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
)

type monitorFunc func(chan<- drainevent.DrainEvent, chan<- drainevent.DrainEvent, config.Config)

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	nthConfig := config.ParseCliArgs()
	err := webhook.ValidateWebhookConfig(nthConfig)
	if err != nil {
		log.Fatalln("Webhook validation failed: ", err)
	}
	node, err := node.New(nthConfig)
	if err != nil {
		log.Fatalln("Unable to instantiate a node for various kubernetes node functions: ", err)
	}
	err = node.UncordonIfLabeled()
	if err != nil {
		log.Println("Unable to complete node label actions: ", err)
	}
	drainEventStore := draineventstore.New(&nthConfig)

	drainChan := make(chan drainevent.DrainEvent)
	defer close(drainChan)
	cancelChan := make(chan drainevent.DrainEvent)
	defer close(cancelChan)

	monitoringFns := []monitorFunc{
		drainevent.MonitorForSpotITNEvents,
		drainevent.MonitorForScheduledEvents,
	}
	for _, fn := range monitoringFns {
		go fn(drainChan, cancelChan, nthConfig)
	}

	go watchForDrainEvents(drainChan, drainEventStore, nthConfig)
	log.Println("Started watching for drain events")
	log.Println("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, drainEventStore, node)
	log.Println("Started watching for event cancellations")

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
			drainIfNecessary(drainEventStore, node, nthConfig)
		}
	}
	log.Println("AWS Node Termination Handler is shutting down")
}

func watchForDrainEvents(drainChan <-chan drainevent.DrainEvent, drainEventStore *draineventstore.Store, nthConfig config.Config) {
	for {
		drainEvent := <-drainChan
		log.Printf("Got drain event from channel %+v\n", drainEvent)
		drainEventStore.AddDrainEvent(&drainEvent)
	}
}

func watchForCancellationEvents(cancelChan <-chan drainevent.DrainEvent, drainEventStore *draineventstore.Store, node *node.Node) {
	for {
		drainEvent := <-cancelChan
		log.Printf("Got cancel event from channel %+v\n", drainEvent)
		drainEventStore.CancelDrainEvent(drainEvent.EventID)
		if drainEventStore.ShouldUncordonNode() {
			node.Uncordon()
		}
	}
}

func drainIfNecessary(drainEventStore *draineventstore.Store, node *node.Node, nthConfig config.Config) {
	if drainEvent, ok := drainEventStore.GetActiveEvent(); ok {
		if drainEvent.PreDrainTask != nil {
			err := drainEvent.PreDrainTask(node)
			if err != nil {
				log.Println("There was a problem executing the pre-drain task: ", err)
			}
		}
		node.Drain()
		drainEventStore.MarkAllAsDrained()
		log.Printf("Node %q successfully drained.\n", nthConfig.NodeName)
		if nthConfig.WebhookURL != "" {
			webhook.Post(drainEvent, nthConfig)
		}
	}
}
