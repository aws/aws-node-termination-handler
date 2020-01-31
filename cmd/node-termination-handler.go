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
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/draineventstore"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
)

const (
	scheduledMaintenance = "Scheduled Maintenance"
	spotITN              = "Spot ITN"
	metadataTries        = 3
)

type monitorFunc func(chan<- drainevent.DrainEvent, chan<- drainevent.DrainEvent, *ec2metadata.EC2MetadataService) error

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	nthConfig, err := config.ParseCliArgs()
	if err != nil {
		log.Fatalln("Failed to parse cli args: ", err)
	}

	err = webhook.ValidateWebhookConfig(nthConfig)
	if err != nil {
		log.Fatalln("Webhook validation failed: ", err)
	}
	node, err := node.New(nthConfig)
	if err != nil {
		log.Fatalln("Unable to instantiate a node for various kubernetes node functions: ", err)
	}

	drainEventStore := draineventstore.New(nthConfig)

	if nthConfig.EnableScheduledEventDraining {
		err = handleRebootUncordon(drainEventStore, *node)
		if err != nil {
			log.Printf("Unable to complete the uncordon after reboot workflow on startup: %v", err)
		}
	}

	drainChan := make(chan drainevent.DrainEvent)
	defer close(drainChan)
	cancelChan := make(chan drainevent.DrainEvent)
	defer close(cancelChan)

	imds := ec2metadata.New(nthConfig.MetadataURL, metadataTries)

	monitoringFns := map[string]monitorFunc{}
	if nthConfig.EnableSpotInterruptionDraining {
		monitoringFns[spotITN] = drainevent.MonitorForSpotITNEvents
	}
	if nthConfig.EnableScheduledEventDraining {
		monitoringFns[scheduledMaintenance] = drainevent.MonitorForScheduledEvents
	}

	for eventType, fn := range monitoringFns {
		go func(monitorFn monitorFunc, eventType string) {
			log.Printf("Started monitoring for %s events", eventType)
			for range time.Tick(time.Second * 2) {
				err := monitorFn(drainChan, cancelChan, imds)
				if err != nil {
					log.Printf("There was a problem monitoring for %s events: %v", eventType, err)
				}
			}
		}(fn, eventType)
	}

	go watchForDrainEvents(drainChan, drainEventStore, nthConfig)
	log.Println("Started watching for drain events")
	log.Println("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, drainEventStore, node)
	log.Println("Started watching for event cancellations")

	for range time.NewTicker(1 * time.Second).C {
		select {
		case _ = <-signalChan:
			// Exit drain loop if a SIGTERM is received or the channel is closed
			break
		default:
			drainIfNecessary(drainEventStore, *node, nthConfig)
		}
	}
	log.Println("AWS Node Termination Handler is shutting down")
}

func handleRebootUncordon(drainEventStore *draineventstore.Store, node node.Node) error {
	isLabeled, err := node.IsLabeledWithAction()
	if err != nil {
		return err
	}
	if !isLabeled {
		return nil
	}
	eventID, err := node.GetEventID()
	if err != nil {
		return err
	}
	err = node.UncordonIfRebooted()
	if err != nil {
		return fmt.Errorf("Unable to complete node label actions: %w", err)
	}
	drainEventStore.IgnoreEvent(eventID)
	return nil
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
			log.Println("Uncordoning the node due to a cancellation event")
			err := node.Uncordon()
			if err != nil {
				log.Printf("Uncordoning the node failed: %v", err)
			}
			node.RemoveNTHLabels()
		} else {
			log.Println("Another drain event is active, not uncordoning the node")
		}
	}
}

func drainIfNecessary(drainEventStore *draineventstore.Store, node node.Node, nthConfig config.Config) {
	if drainEvent, ok := drainEventStore.GetActiveEvent(); ok {
		if drainEvent.PreDrainTask != nil {
			err := drainEvent.PreDrainTask(*drainEvent, node)
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
