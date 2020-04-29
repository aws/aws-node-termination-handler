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
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
)

const (
	scheduledMaintenance = "Scheduled Maintenance"
	spotITN              = "Spot ITN"
)

type monitorFunc func(chan<- interruptionevent.InterruptionEvent, chan<- interruptionevent.InterruptionEvent, *ec2metadata.Service) error

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

	imds := ec2metadata.New(nthConfig.MetadataURL, nthConfig.MetadataTries)

	interruptionEventStore := interruptioneventstore.New(nthConfig)
	nodeMetadata := imds.GetNodeMetadata()

	if nthConfig.EnableScheduledEventDraining {
		err = handleRebootUncordon(interruptionEventStore, *node)
		if err != nil {
			log.Printf("Unable to complete the uncordon after reboot workflow on startup: %v\n", err)
		}
	}

	interruptionChan := make(chan interruptionevent.InterruptionEvent)
	defer close(interruptionChan)
	cancelChan := make(chan interruptionevent.InterruptionEvent)
	defer close(cancelChan)

	monitoringFns := map[string]monitorFunc{}
	if nthConfig.EnableSpotInterruptionDraining {
		monitoringFns[spotITN] = interruptionevent.MonitorForSpotITNEvents
	}
	if nthConfig.EnableScheduledEventDraining {
		monitoringFns[scheduledMaintenance] = interruptionevent.MonitorForScheduledEvents
	}

	for eventType, fn := range monitoringFns {
		go func(monitorFn monitorFunc, eventType string) {
			log.Printf("Started monitoring for %s events", eventType)
			for range time.Tick(time.Second * 2) {
				err := monitorFn(interruptionChan, cancelChan, imds)
				if err != nil {
					log.Printf("There was a problem monitoring for %s events: %v", eventType, err)
				}
			}
		}(fn, eventType)
	}

	go watchForInterruptionEvents(interruptionChan, interruptionEventStore, nodeMetadata)
	log.Println("Started watching for interruption events")
	log.Println("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, interruptionEventStore, node, nodeMetadata)
	log.Println("Started watching for event cancellations")

	for range time.NewTicker(1 * time.Second).C {
		select {
		case _ = <-signalChan:
			// Exit interruption loop if a SIGTERM is received or the channel is closed
			break
		default:
			drainOrCordonIfNecessary(interruptionEventStore, *node, nthConfig, nodeMetadata)
		}
	}
	log.Println("AWS Node Termination Handler is shutting down")
}

func handleRebootUncordon(interruptionEventStore *interruptioneventstore.Store, node node.Node) error {
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
	interruptionEventStore.IgnoreEvent(eventID)
	return nil
}

func watchForInterruptionEvents(interruptionChan <-chan interruptionevent.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, nodeMetadata ec2metadata.NodeMetadata) {
	for {
		interruptionEvent := <-interruptionChan
		log.Printf("Got interruption event from channel %+v %+v\n", nodeMetadata, interruptionEvent)
		interruptionEventStore.AddInterruptionEvent(&interruptionEvent)
	}
}

func watchForCancellationEvents(cancelChan <-chan interruptionevent.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, node *node.Node, nodeMetadata ec2metadata.NodeMetadata) {
	for {
		interruptionEvent := <-cancelChan
		log.Printf("Got cancel event from channel %+v %+v\n", nodeMetadata, interruptionEvent)
		interruptionEventStore.CancelInterruptionEvent(interruptionEvent.EventID)
		if interruptionEventStore.ShouldUncordonNode() {
			log.Println("Uncordoning the node due to a cancellation event")
			err := node.Uncordon()
			if err != nil {
				log.Printf("Uncordoning the node failed: %v", err)
			}
			node.RemoveNTHLabels()
		} else {
			log.Println("Another interruption event is active, not uncordoning the node")
		}
	}
}

func drainOrCordonIfNecessary(interruptionEventStore *interruptioneventstore.Store, node node.Node, nthConfig config.Config, nodeMetadata ec2metadata.NodeMetadata) {
	if drainEvent, ok := interruptionEventStore.GetActiveEvent(); ok {
		if drainEvent.PreDrainTask != nil {
			err := drainEvent.PreDrainTask(*drainEvent, node)
			if err != nil {
				log.Println("There was a problem executing the pre-drain task: ", err)
			}
		}
		if nthConfig.CordonOnly {
			err := node.Cordon()
			if err != nil {
				log.Println("There was a problem while trying to cordon the node: ", err)
				os.Exit(1)
			}
			log.Printf("Node %q successfully cordoned.\n", nthConfig.NodeName)
		} else {
			err := node.CordonAndDrain()
			if err != nil {
				log.Println("There was a problem while trying to cordon and drain the node: ", err)
				os.Exit(1)
			}
			log.Printf("Node %q successfully cordoned and drained.\n", nthConfig.NodeName)
		}
		interruptionEventStore.MarkAllAsDrained()
		if nthConfig.WebhookURL != "" {
			webhook.Post(nodeMetadata, drainEvent, nthConfig)
		}
	}
}
