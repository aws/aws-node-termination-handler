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
	"errors"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	drainEventStore := draineventstore.New(&nthConfig)

	log.Println("Kubernetes AWS Node Termination Handler has started successfully!")
	drainChan := make(chan drainevent.DrainEvent)
	defer close(drainChan)
	cancelChan := make(chan drainevent.DrainEvent)
	defer close(cancelChan)
	monitoringFns := []func(chan<- drainevent.DrainEvent, chan<- drainevent.DrainEvent, config.Config){}
	if nthConfig.EnableSpotInterruptionDraining {
		monitoringFns = append(monitoringFns, draineventstore.MonitorForSpotITNEvents)
	}
	if nthConfig.EnableScheduledEventDraining {
		monitoringFns = append(monitoringFns, draineventstore.MonitorForScheduledEvents)
	}
	for _, fn := range monitoringFns {
		go fn(drainChan, cancelChan, nthConfig)
	}

	go watchForDrainEvents(drainChan, drainEventStore, nthConfig)
	log.Println("Started watching for drain events")

	go watchForCancellationEvents(cancelChan, drainEventStore, nthConfig)
	log.Println("Started watching for event cancellations")

	err := uncordonIfNodeIsLabeled()
	if err != nil {
		log.Printf("Uncordon on startup failed. %s\n", err.Error())
	}

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
	log.Println("AWS Node Termination Handler is shutting down")
}

func watchForDrainEvents(drainChan <-chan drainevent.DrainEvent, drainEventStore *draineventstore.Store, nthConfig config.Config) {
	for {
		drainEvent := <-drainChan
		log.Printf("Got drain event from channel %+v\n", drainEvent)
		drainEventStore.AddDrainEvent(&drainEvent)
	}
}

func drainIfNecessary(drainEventStore *draineventstore.Store, nthConfig config.Config) {
	if drainEvent, ok := drainEventStore.GetActiveEvent(); ok {
		if drainEvent.PreDrainTask != nil {
			drainEvent.PreDrainTask()
		}
		drainer.Drain()
		drainEventStore.MarkAllAsDrained()
		log.Printf("Node %q successfully drained.\n", nthConfig.NodeName)
		if nthConfig.WebhookURL != "" {
			webhook.Post(drainEvent, nthConfig)
		}
	}
}

func watchForCancellationEvents(cancelChan <-chan drainevent.DrainEvent, drainEventStore *draineventstore.Store, nthConfig config.Config) {
	for {
		drainEvent := <-cancelChan
		drainEventStore.CancelDrainEvent(drainEvent.EventID)
		if drainEventStore.ShouldUncordonNode() {
			drainer.UncordonNode()
		}
	}
}

// uncordonIfNodeIsLabeled will check for node labels to trigger an uncordon because of a system-reboot scheduled event
func uncordonIfNodeIsLabeled() error {
	returnErr := errors.New("Unable to uncordon labeled node")
	node, err := drainer.FetchNode()
	if err != nil {
		log.Printf("There was a problem fetching the node while running the action label handler. Error: %v\n", err)
		return returnErr
	}
	timeVal, ok := node.Labels[drainevent.ActionLabelTimeKey]
	if !ok {
		log.Printf("There was no %s label found requiring action label handling\n", drainevent.ActionLabelTimeKey)
		return nil
	}
	timeValNum, err := strconv.ParseInt(timeVal, 10, 64)
	if err != nil {
		log.Printf("Cannot convert unix time \"%s\" from label %s to int64\n", timeVal, drainevent.ActionLabelTimeKey)
		return returnErr
	}
	secSinceLabel := time.Now().Unix() - timeValNum
	switch actionVal := node.Labels[drainevent.ActionLabelKey]; actionVal {
	case drainevent.UncordonAfterRebootLabelVal:
		log.Printf("Handling action label on start for task %s\n", actionVal)
		data, err := ioutil.ReadFile("/proc/uptime")
		if err != nil {
			log.Printf("Not able to read /proc/uptime while handling an %s action. Error: %v\n", drainevent.UncordonAfterRebootLabelVal, err)
			return returnErr
		}

		uptime, err := strconv.ParseFloat(strings.Split(string(data), " ")[0], 64)
		if err != nil {
			log.Printf("Could not parse /proc/uptime's first number to retrieve the system uptime. Error: %v\n", err)
			return returnErr
		}
		if secSinceLabel < int64(uptime) {
			log.Printf("The system has not restarted yet. The last restart was %d secs ago and the label was applied %d sec ago\n", int64(uptime), secSinceLabel)
			return returnErr
		}
		err = drainer.UncordonNode()
		if err != nil {
			log.Printf("Unable to uncordon node. Error: %v\n", err)
			return returnErr
		}
		log.Printf("Successfully completed action %s.\n", drainevent.UncordonAfterRebootLabelVal)
	default:
		log.Println("There are no label actions to handle.")
	}
	return nil
}
