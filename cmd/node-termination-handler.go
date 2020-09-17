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
	"syscall"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	scheduledMaintenance  = "Scheduled Maintenance"
	spotITN               = "Spot ITN"
	timeFormat            = "2006/01/02 15:04:05"
	duplicateErrThreshold = 3
)

func main() {
	// Zerolog uses json formatting by default, so change that to a human-readable format instead
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: timeFormat, NoColor: true, FormatLevel: logFormatLevel})

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	nthConfig, err := config.ParseCliArgs()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse cli args,")
	}

	if nthConfig.JsonLogging {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
		printJsonConfigArgs(nthConfig)
	} else {
		printHumanConfigArgs(nthConfig)
	}

	err = webhook.ValidateWebhookConfig(nthConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Webhook validation failed,")
	}
	node, err := node.New(nthConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to instantiate a node for various kubernetes node functions,")
	}

	metrics, err := observability.InitMetrics(nthConfig.EnablePrometheus, nthConfig.PrometheusPort)
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to instantiate observability metrics,")
	}

	imds := ec2metadata.New(nthConfig.MetadataURL, nthConfig.MetadataTries)

	interruptionEventStore := interruptioneventstore.New(nthConfig)
	nodeMetadata := imds.GetNodeMetadata()

	if nthConfig.EnableScheduledEventDraining {
		err = handleRebootUncordon(nthConfig.NodeName, interruptionEventStore, *node)
		if err != nil {
			log.Log().Err(err).Msg("Unable to complete the uncordon after reboot workflow on startup")
		}
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
					if err == previousErr {
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

	for range time.NewTicker(1 * time.Second).C {
		select {
		case _ = <-signalChan:
			// Exit interruption loop if a SIGTERM is received or the channel is closed
			break
		default:
			drainOrCordonIfNecessary(interruptionEventStore, *node, nthConfig, nodeMetadata, metrics)
		}
	}
	log.Log().Msg("AWS Node Termination Handler is shutting down")
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

func drainOrCordonIfNecessary(interruptionEventStore *interruptioneventstore.Store, node node.Node, nthConfig config.Config, nodeMetadata ec2metadata.NodeMetadata, metrics observability.Metrics) {
	if drainEvent, ok := interruptionEventStore.GetActiveEvent(); ok {
		nodeName := drainEvent.NodeName
		if drainEvent.PreDrainTask != nil {
			err := drainEvent.PreDrainTask(*drainEvent, node)
			if err != nil {
				log.Log().Err(err).Msg("There was a problem executing the pre-drain task")
			}
			metrics.NodeActionsInc("pre-drain", nodeName, err)
		}

		if nthConfig.CordonOnly {
			err := node.Cordon(nodeName)
			if err != nil {
				log.Log().Err(err).Msg("There was a problem while trying to cordon the node")
				os.Exit(1)
			}
			log.Log().Str("node_name", nodeName).Msg("Node successfully cordoned")
			metrics.NodeActionsInc("cordon", nodeName, err)
		} else {
			err := node.CordonAndDrain(nodeName)
			if err != nil {
				log.Log().Err(err).Msg("There was a problem while trying to cordon and drain the node")
				os.Exit(1)
			}
			log.Log().Str("node_name", nodeName).Msg("Node successfully cordoned and drained")
			metrics.NodeActionsInc("cordon-and-drain", nodeName, err)
		}

		interruptionEventStore.MarkAllAsDrained(nodeName)
		if nthConfig.WebhookURL != "" {
			webhook.Post(nodeMetadata, drainEvent, nthConfig)
		}
	}
}

func logFormatLevel(interface{}) string {
	return ""
}

func printJsonConfigArgs(config config.Config) {
	// manually setting fields instead of using log.Log().Interface() to use snake_case instead of PascalCase
	// intentionally did not log webhook configuration as there may be secrets
	log.Log().
		Bool("dry_run", config.DryRun).
		Str("node_name", config.NodeName).
		Str("metadata_url", config.MetadataURL).
		Str("kubernetes_service_host", config.KubernetesServiceHost).
		Str("kubernetes_service_port", config.KubernetesServicePort).
		Bool("delete_local_data", config.DeleteLocalData).
		Bool("ignore_daemon_sets", config.IgnoreDaemonSets).
		Int("pod_termination_grace_period", config.PodTerminationGracePeriod).
		Int("node_termination_grace_period", config.NodeTerminationGracePeriod).
		Bool("enable_scheduled_event_draining", config.EnableScheduledEventDraining).
		Bool("enable_spot_interruption_draining", config.EnableSpotInterruptionDraining).
		Int("metadata_tries", config.MetadataTries).
		Bool("cordon_only", config.CordonOnly).
		Bool("taint_node", config.TaintNode).
		Bool("json_logging", config.JsonLogging).
		Str("webhook_proxy", config.WebhookProxy).
		Str("uptime_from_file", config.UptimeFromFile).
		Bool("enable_prometheus_server", config.EnablePrometheus).
		Int("prometheus_server_port", config.PrometheusPort).
		Msg("aws-node-termination-handler arguments")
}

func printHumanConfigArgs(config config.Config) {
	// intentionally did not log webhook configuration as there may be secrets
	log.Log().Msgf(
		"aws-node-termination-handler arguments: \n"+
			"\tdry-run: %t,\n"+
			"\tnode-name: %s,\n"+
			"\tmetadata-url: %s,\n"+
			"\tkubernetes-service-host: %s,\n"+
			"\tkubernetes-service-port: %s,\n"+
			"\tdelete-local-data: %t,\n"+
			"\tignore-daemon-sets: %t,\n"+
			"\tpod-termination-grace-period: %d,\n"+
			"\tnode-termination-grace-period: %d,\n"+
			"\tenable-scheduled-event-draining: %t,\n"+
			"\tenable-spot-interruption-draining: %t,\n"+
			"\tmetadata-tries: %d,\n"+
			"\tcordon-only: %t,\n"+
			"\ttaint-node: %t,\n"+
			"\tjson-logging: %t,\n"+
			"\twebhook-proxy: %s,\n"+
			"\tuptime-from-file: %s,\n"+
			"\tenable-prometheus-server: %t,\n"+
			"\tprometheus-server-port: %d,\n",
		config.DryRun,
		config.NodeName,
		config.MetadataURL,
		config.KubernetesServiceHost,
		config.KubernetesServicePort,
		config.DeleteLocalData,
		config.IgnoreDaemonSets,
		config.PodTerminationGracePeriod,
		config.NodeTerminationGracePeriod,
		config.EnableScheduledEventDraining,
		config.EnableSpotInterruptionDraining,
		config.MetadataTries,
		config.CordonOnly,
		config.TaintNode,
		config.JsonLogging,
		config.WebhookProxy,
		config.UptimeFromFile,
		config.EnablePrometheus,
		config.PrometheusPort,
	)
}
