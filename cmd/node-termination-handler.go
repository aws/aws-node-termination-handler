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
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/rebalancerecommendation"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	scheduledMaintenance    = "Scheduled Maintenance"
	spotITN                 = "Spot ITN"
	rebalanceRecommendation = "Rebalance Recommendation"
	sqsEvents               = "SQS Event"
	timeFormat              = "2006/01/02 15:04:05"
	duplicateErrThreshold   = 3
)

func main() {
	// Zerolog uses json formatting by default, so change that to a human-readable format instead
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: timeFormat, NoColor: true})

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	nthConfig, err := config.ParseCliArgs()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse cli args,")
	}

	if nthConfig.JsonLogging {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
	switch strings.ToLower(nthConfig.LogLevel) {
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}

	err = webhook.ValidateWebhookConfig(nthConfig)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Webhook validation failed,")
	}
	node, err := node.New(nthConfig)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to instantiate a node for various kubernetes node functions,")
	}

	metrics, err := observability.InitMetrics(nthConfig.EnablePrometheus, nthConfig.PrometheusPort)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to instantiate observability metrics,")
	}

	imds := ec2metadata.New(nthConfig.MetadataURL, nthConfig.MetadataTries)

	interruptionEventStore := interruptioneventstore.New(nthConfig)
	nodeMetadata := imds.GetNodeMetadata()
	// Populate the aws region if available from node metadata and not already explicitly configured
	if nthConfig.AWSRegion == "" && nodeMetadata.Region != "" {
		nthConfig.AWSRegion = nodeMetadata.Region
		if nthConfig.AWSSession != nil {
			nthConfig.AWSSession.Config.Region = &nodeMetadata.Region
		}
	} else if nthConfig.AWSRegion == "" && nodeMetadata.Region == "" && nthConfig.EnableSQSTerminationDraining {
		nthConfig.Print()
		log.Fatal().Msgf("Unable to find the AWS region to process queue events.")
	}
	nthConfig.Print()

	if nthConfig.EnableScheduledEventDraining {
		stopCh := make(chan struct{})
		go func() {
			time.Sleep(8 * time.Second)
			stopCh <- struct{}{}
		}()
		//will retry 4 times with an interval of 2 seconds.
		wait.PollImmediateUntil(2*time.Second, func() (done bool, err error) {
			err = handleRebootUncordon(nthConfig.NodeName, interruptionEventStore, *node)
			if err != nil {
				log.Log().Err(err).Msgf("Unable to complete the uncordon after reboot workflow on startup, retrying")
			}
			return false, nil
		}, stopCh)
	}

	interruptionChan := make(chan monitor.InterruptionEvent)
	defer close(interruptionChan)
	cancelChan := make(chan monitor.InterruptionEvent)
	defer close(cancelChan)

	monitoringFns := map[string]monitor.Monitor{}
	if nthConfig.EnableSpotInterruptionDraining {
		imdsSpotMonitor := spotitn.NewSpotInterruptionMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
		monitoringFns[spotITN] = imdsSpotMonitor
	}
	if nthConfig.EnableScheduledEventDraining {
		imdsScheduledEventMonitor := scheduledevent.NewScheduledEventMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
		monitoringFns[scheduledMaintenance] = imdsScheduledEventMonitor
	}
	if nthConfig.EnableRebalanceMonitoring {
		imdsRebalanceMonitor := rebalancerecommendation.NewRebalanceRecommendationMonitor(imds, interruptionChan, nthConfig.NodeName)
		monitoringFns[rebalanceRecommendation] = imdsRebalanceMonitor
	}
	if nthConfig.EnableSQSTerminationDraining {
		creds, err := nthConfig.AWSSession.Config.Credentials.Get()
		if err != nil {
			log.Warn().Err(err).Msg("Unable to get AWS credentials")
		}
		log.Debug().Msgf("AWS Credentials retrieved from provider: %s", creds.ProviderName)

		sqsMonitor := sqsevent.SQSMonitor{
			CheckIfManaged:   nthConfig.CheckASGTagBeforeDraining,
			ManagedAsgTag:    nthConfig.ManagedAsgTag,
			QueueURL:         nthConfig.QueueURL,
			InterruptionChan: interruptionChan,
			CancelChan:       cancelChan,
			SQS:              sqs.New(nthConfig.AWSSession),
			ASG:              autoscaling.New(nthConfig.AWSSession),
			EC2:              ec2.New(nthConfig.AWSSession),
		}
		monitoringFns[sqsEvents] = sqsMonitor
	}

	for _, fn := range monitoringFns {
		go func(monitor monitor.Monitor) {
			log.Log().Str("event_type", monitor.Kind()).Msg("Started monitoring for events")
			var previousErr error
			var duplicateErrCount int
			for range time.Tick(time.Second * 2) {
				err := monitor.Monitor()
				if err != nil {
					log.Log().Str("event_type", monitor.Kind()).Err(err).Msg("There was a problem monitoring for events")
					metrics.ErrorEventsInc(monitor.Kind())
					if previousErr != nil && err.Error() == previousErr.Error() {
						duplicateErrCount++
					} else {
						duplicateErrCount = 0
						previousErr = err
					}
					if duplicateErrCount >= duplicateErrThreshold {
						log.Log().Msg("Stopping NTH - Duplicate Error Threshold hit.")
						panic(fmt.Sprintf("%v", err))
					}
				}
			}
		}(fn)
	}

	go watchForInterruptionEvents(interruptionChan, interruptionEventStore)
	log.Log().Msg("Started watching for interruption events")
	log.Log().Msg("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, interruptionEventStore, node, metrics)
	log.Log().Msg("Started watching for event cancellations")

	var wg sync.WaitGroup

	for range time.NewTicker(1 * time.Second).C {
		select {
		case <-signalChan:
			// Exit interruption loop if a SIGTERM is received or the channel is closed
			break
		default:
			for event, ok := interruptionEventStore.GetActiveEvent(); ok && !event.InProgress; event, ok = interruptionEventStore.GetActiveEvent() {
				select {
				case interruptionEventStore.Workers <- 1:
					event.InProgress = true
					wg.Add(1)
					go drainOrCordonIfNecessary(interruptionEventStore, event, *node, nthConfig, nodeMetadata, metrics, &wg)
				default:
					log.Warn().Msg("all workers busy, waiting")
					break
				}
			}
		}
	}
	log.Log().Msg("AWS Node Termination Handler is shutting down")
	wg.Wait()
	log.Debug().Msg("all event processors finished")
}

func handleRebootUncordon(nodeName string, interruptionEventStore *interruptioneventstore.Store, node node.Node) error {
	isLabeled, err := node.IsLabeledWithAction(nodeName)
	if err != nil {
		return err
	}
	if !isLabeled {
		return nil
	}
	eventID, err := node.GetEventID(nodeName)
	if err != nil {
		return err
	}
	err = node.UncordonIfRebooted(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to complete node label actions: %w", err)
	}
	interruptionEventStore.IgnoreEvent(eventID)
	return nil
}

func watchForInterruptionEvents(interruptionChan <-chan monitor.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store) {
	for {
		interruptionEvent := <-interruptionChan
		interruptionEventStore.AddInterruptionEvent(&interruptionEvent)
	}
}

func watchForCancellationEvents(cancelChan <-chan monitor.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, node *node.Node, metrics observability.Metrics) {
	for {
		interruptionEvent := <-cancelChan
		nodeName := interruptionEvent.NodeName
		interruptionEventStore.CancelInterruptionEvent(interruptionEvent.EventID)
		if interruptionEventStore.ShouldUncordonNode(nodeName) {
			log.Log().Msg("Uncordoning the node due to a cancellation event")
			err := node.Uncordon(nodeName)
			if err != nil {
				log.Log().Err(err).Msg("Uncordoning the node failed")
			}
			metrics.NodeActionsInc("uncordon", nodeName, err)

			node.RemoveNTHLabels(nodeName)
			node.RemoveNTHTaints(nodeName)
		} else {
			log.Log().Msg("Another interruption event is active, not uncordoning the node")
		}
	}
}

func drainOrCordonIfNecessary(interruptionEventStore *interruptioneventstore.Store, drainEvent *monitor.InterruptionEvent, node node.Node, nthConfig config.Config, nodeMetadata ec2metadata.NodeMetadata, metrics observability.Metrics, wg *sync.WaitGroup) {
	defer wg.Done()
	nodeName := drainEvent.NodeName
	nodeLabels, err := node.GetNodeLabels(nodeName)
	if err != nil {
		log.Warn().Err(err).Msgf("Unable to fetch node labels for node '%s' ", nodeName)
	}
	drainEvent.NodeLabels = nodeLabels
	if drainEvent.PreDrainTask != nil {
		err := drainEvent.PreDrainTask(*drainEvent, node)
		if err != nil {
			log.Log().Err(err).Msg("There was a problem executing the pre-drain task")
		}
		metrics.NodeActionsInc("pre-drain", nodeName, err)
	}

	if nthConfig.CordonOnly || drainEvent.IsRebalanceRecommendation() {
		err := node.Cordon(nodeName)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Warn().Err(err).Msgf("node '%s' not found in the cluster", nodeName)
			} else {
				log.Log().Err(err).Msg("There was a problem while trying to cordon the node")
				os.Exit(1)
			}
		} else {
			log.Log().Str("node_name", nodeName).Msg("Node successfully cordoned")
			podNameList, err := node.FetchPodNameList(nodeName)
			if err != nil {
				log.Log().Err(err).Msgf("Unable to fetch running pods for node '%s' ", nodeName)
			}
			drainEvent.Pods = podNameList
			err = node.LogPods(podNameList, nodeName)
			if err != nil {
				log.Log().Err(err).Msg("There was a problem while trying to log all pod names on the node")
			}
			metrics.NodeActionsInc("cordon", nodeName, err)
		}
	} else {
		err := node.CordonAndDrain(nodeName)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Warn().Err(err).Msgf("node '%s' not found in the cluster", nodeName)
			} else {
				log.Log().Err(err).Msg("There was a problem while trying to cordon and drain the node")
				os.Exit(1)
			}
		} else {
			log.Log().Str("node_name", nodeName).Msg("Node successfully cordoned and drained")
			metrics.NodeActionsInc("cordon-and-drain", nodeName, err)
		}
	}

	interruptionEventStore.MarkAllAsDrained(nodeName)
	if nthConfig.WebhookURL != "" {
		webhook.Post(nodeMetadata, drainEvent, nthConfig)
	}
	if drainEvent.PostDrainTask != nil {
		err := drainEvent.PostDrainTask(*drainEvent, node)
		if err != nil {
			log.Err(err).Msg("There was a problem executing the post-drain task")
		}
		metrics.NodeActionsInc("post-drain", nodeName, err)
	}
	<-interruptionEventStore.Workers

}
