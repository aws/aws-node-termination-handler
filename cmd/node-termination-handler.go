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
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent/asg/launch"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent/draincordon"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/asglifecycle"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/rebalancerecommendation"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	scheduledMaintenance    = "Scheduled Maintenance"
	spotITN                 = "Spot ITN"
	asgLifecycle            = "ASG Lifecycle"
	rebalanceRecommendation = "Rebalance Recommendation"
	sqsEvents               = "SQS Event"
	timeFormat              = "2006/01/02 15:04:05"
	duplicateErrThreshold   = 3
)

type interruptionEventHandler interface {
	HandleEvent(*monitor.InterruptionEvent) error
}

func main() {
	// Zerolog uses json formatting by default, so change that to a human-readable format instead
	log.Logger = log.Output(logging.RoutingLevelWriter{
		Writer:    &zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat, NoColor: true},
		ErrWriter: &zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: timeFormat, NoColor: true},
	})

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

	klog.SetLogger(zerologr.New(&log.Logger))

	log.Info().Msgf("Using log format version %d", nthConfig.LogFormatVersion)
	if err = logging.SetFormatVersion(nthConfig.LogFormatVersion); err != nil {
		log.Warn().Err(err).Send()
	}
	if err = observability.SetReasonForKindVersion(nthConfig.LogFormatVersion); err != nil {
		log.Warn().Err(err).Send()
	}

	err = webhook.ValidateWebhookConfig(nthConfig)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Webhook validation failed,")
	}

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal().Err(err).Msgf("retreiving cluster config")
	}
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		log.Fatal().Err(err).Msgf("creating new clientset with config: %v", err)
	}
	node, err := node.New(nthConfig, clientset)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to instantiate a node for various kubernetes node functions,")
	}

	metrics, initMetricsErr := observability.InitMetrics(nthConfig.EnablePrometheus, nthConfig.PrometheusPort)
	if initMetricsErr != nil {
		nthConfig.Print()
		log.Fatal().Err(initMetricsErr).Msg("Unable to instantiate observability metrics,")
	}

	err = observability.InitProbes(nthConfig.EnableProbes, nthConfig.ProbesPort, nthConfig.ProbesEndpoint)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to instantiate probes service,")
	}
	imdsDisabled := nthConfig.EnableSQSTerminationDraining

	interruptionEventStore := interruptioneventstore.New(nthConfig)
	var imds *ec2metadata.Service
	var nodeMetadata ec2metadata.NodeMetadata

	if !imdsDisabled {
		imds = ec2metadata.New(nthConfig.MetadataURL, nthConfig.MetadataTries)
		nodeMetadata = imds.GetNodeMetadata()
	}

	// Populate the aws region if available from node metadata and not already explicitly configured
	if nthConfig.AWSRegion == "" && nodeMetadata.Region != "" {
		nthConfig.AWSRegion = nodeMetadata.Region
	} else if nthConfig.AWSRegion == "" && nthConfig.QueueURL != "" {
		nthConfig.AWSRegion = getRegionFromQueueURL(nthConfig.QueueURL)
		log.Debug().Str("Retrieved AWS region from queue-url: \"%s\"", nthConfig.AWSRegion)
	}
	if nthConfig.AWSRegion == "" && nthConfig.EnableSQSTerminationDraining {
		nthConfig.Print()
		log.Fatal().Msgf("Unable to find the AWS region to process queue events.")
	}

	recorder, err := observability.InitK8sEventRecorder(nthConfig.EmitKubernetesEvents, nthConfig.NodeName, nthConfig.EnableSQSTerminationDraining, nodeMetadata, nthConfig.KubernetesEventsExtraAnnotations, clientset)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to create Kubernetes event recorder,")
	}

	nthConfig.Print()

	if !imdsDisabled && nthConfig.EnableScheduledEventDraining {
		//will retry 4 times with an interval of 2 seconds.
		pollCtx, cancelPollCtx := context.WithTimeout(context.Background(), 8*time.Second)
		err = wait.PollUntilContextCancel(pollCtx, 2*time.Second, true, func(context.Context) (done bool, err error) {
			err = handleRebootUncordon(nthConfig.NodeName, interruptionEventStore, *node)
			if err != nil {
				log.Warn().Err(err).Msgf("Unable to complete the uncordon after reboot workflow on startup, retrying")
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			log.Warn().Err(err).Msgf("All retries failed, unable to complete the uncordon after reboot workflow")
		}
		cancelPollCtx()
	}

	interruptionChan := make(chan monitor.InterruptionEvent)
	defer close(interruptionChan)
	cancelChan := make(chan monitor.InterruptionEvent)
	defer close(cancelChan)

	monitoringFns := map[string]monitor.Monitor{}
	if !imdsDisabled {
		if nthConfig.EnableSpotInterruptionDraining {
			imdsSpotMonitor := spotitn.NewSpotInterruptionMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
			monitoringFns[spotITN] = imdsSpotMonitor
		}
		if nthConfig.EnableASGLifecycleDraining {
			asgLifecycleMonitor := asglifecycle.NewASGLifecycleMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
			monitoringFns[asgLifecycle] = asgLifecycleMonitor
		}
		if nthConfig.EnableScheduledEventDraining {
			imdsScheduledEventMonitor := scheduledevent.NewScheduledEventMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
			monitoringFns[scheduledMaintenance] = imdsScheduledEventMonitor
		}
		if nthConfig.EnableRebalanceMonitoring || nthConfig.EnableRebalanceDraining {
			imdsRebalanceMonitor := rebalancerecommendation.NewRebalanceRecommendationMonitor(imds, interruptionChan, nthConfig.NodeName)
			monitoringFns[rebalanceRecommendation] = imdsRebalanceMonitor
		}
	}
	if nthConfig.EnableSQSTerminationDraining {
		cfg := aws.NewConfig().WithRegion(nthConfig.AWSRegion).WithEndpoint(nthConfig.AWSEndpoint).WithSTSRegionalEndpoint(endpoints.RegionalSTSEndpoint)
		sess := session.Must(session.NewSessionWithOptions(session.Options{
			Config:            *cfg,
			SharedConfigState: session.SharedConfigEnable,
		}))
		creds, err := sess.Config.Credentials.Get()
		if err != nil {
			log.Fatal().Err(err).Msg("Unable to get AWS credentials")
		}
		log.Debug().Msgf("AWS Credentials retrieved from provider: %s", creds.ProviderName)

		ec2Client := ec2.New(sess)

		if initMetricsErr == nil && nthConfig.EnablePrometheus {
			go metrics.InitNodeMetrics(nthConfig, node, ec2Client)
		}

		completeLifecycleActionDelay := time.Duration(nthConfig.CompleteLifecycleActionDelaySeconds) * time.Second
		sqsMonitor := sqsevent.SQSMonitor{
			CheckIfManaged:                nthConfig.CheckTagBeforeDraining,
			ManagedTag:                    nthConfig.ManagedTag,
			QueueURL:                      nthConfig.QueueURL,
			InterruptionChan:              interruptionChan,
			CancelChan:                    cancelChan,
			SQS:                           sqsevent.GetSqsClient(sess),
			ASG:                           autoscaling.New(sess),
			EC2:                           ec2Client,
			BeforeCompleteLifecycleAction: func() { <-time.After(completeLifecycleActionDelay) },
		}
		monitoringFns[sqsEvents] = sqsMonitor
	}

	for _, fn := range monitoringFns {
		go func(monitor monitor.Monitor) {
			logging.VersionedMsgs.MonitoringStarted(monitor.Kind())
			var previousErr error
			var duplicateErrCount int
			for range time.Tick(time.Second * 2) {
				err := monitor.Monitor()
				if err != nil {
					logging.VersionedMsgs.ProblemMonitoringForEvents(monitor.Kind(), err)
					metrics.ErrorEventsInc(monitor.Kind())
					recorder.Emit(nthConfig.NodeName, observability.Warning, observability.MonitorErrReason, observability.MonitorErrMsgFmt, monitor.Kind())
					if previousErr != nil && err.Error() == previousErr.Error() {
						duplicateErrCount++
					} else {
						duplicateErrCount = 0
						previousErr = err
					}
					if duplicateErrCount >= duplicateErrThreshold {
						log.Warn().Msg("Stopping NTH - Duplicate Error Threshold hit.")
						panic(fmt.Sprintf("%v", err))
					}
				}
			}
		}(fn)
	}

	go watchForInterruptionEvents(interruptionChan, interruptionEventStore)
	log.Info().Msg("Started watching for interruption events")
	log.Info().Msg("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, interruptionEventStore, node, metrics, recorder)
	log.Info().Msg("Started watching for event cancellations")

	var wg sync.WaitGroup

	asgLaunchHandler := launch.New(interruptionEventStore, *node, nthConfig, metrics, recorder, clientset)
	drainCordonHander := draincordon.New(interruptionEventStore, *node, nthConfig, nodeMetadata, metrics, recorder)

	for range time.NewTicker(1 * time.Second).C {
		select {
		case <-signalChan:
			// Exit interruption loop if a SIGTERM is received or the channel is closed
			break
		default:
		EventLoop:
			for event, ok := interruptionEventStore.GetActiveEvent(); ok; event, ok = interruptionEventStore.GetActiveEvent() {
				select {
				case interruptionEventStore.Workers <- 1:
					logging.VersionedMsgs.RequestingInstanceDrain(event)
					event.InProgress = true
					wg.Add(1)
					recorder.Emit(event.NodeName, observability.Normal, observability.GetReasonForKind(event.Kind, event.Monitor), event.Description)
					go processInterruptionEvent(interruptionEventStore, event, []interruptionEventHandler{asgLaunchHandler, drainCordonHander}, &wg)
				default:
					log.Warn().Msg("all workers busy, waiting")
					break EventLoop
				}
			}
		}
	}
	log.Info().Msg("AWS Node Termination Handler is shutting down")
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

func watchForCancellationEvents(cancelChan <-chan monitor.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, node *node.Node, metrics observability.Metrics, recorder observability.K8sEventRecorder) {
	for {
		interruptionEvent := <-cancelChan
		nodeName := interruptionEvent.NodeName
		eventID := interruptionEvent.EventID
		interruptionEventStore.CancelInterruptionEvent(interruptionEvent.EventID)
		if interruptionEventStore.ShouldUncordonNode(nodeName) {
			log.Info().Msg("Uncordoning the node due to a cancellation event")
			err := node.Uncordon(nodeName)
			if err != nil {
				log.Err(err).Msg("Uncordoning the node failed")
				recorder.Emit(nodeName, observability.Warning, observability.UncordonErrReason, observability.UncordonErrMsgFmt, err.Error())
			} else {
				recorder.Emit(nodeName, observability.Normal, observability.UncordonReason, observability.UncordonMsg)
			}
			metrics.NodeActionsInc("uncordon", nodeName, eventID, err)

			err = node.RemoveNTHLabels(nodeName)
			if err != nil {
				log.Warn().Err(err).Msg("There was an issue removing NTH labels from node")
			}

			err = node.RemoveNTHTaints(nodeName)
			if err != nil {
				log.Warn().Err(err).Msg("There was an issue removing NTH taints from node")
			}
		} else {
			log.Info().Msg("Another interruption event is active, not uncordoning the node")
		}
	}
}

func processInterruptionEvent(interruptionEventStore *interruptioneventstore.Store, event *monitor.InterruptionEvent, eventHandlers []interruptionEventHandler, wg *sync.WaitGroup) {
	defer wg.Done()

	if event == nil {
		log.Error().Msg("processing nil interruption event")
		<-interruptionEventStore.Workers
		return
	}

	var err error
	for _, eventHandler := range eventHandlers {
		err = eventHandler.HandleEvent(event)
		if err != nil {
			log.Error().Err(err).Interface("event", event).Msg("handling event")
		}
	}
	<-interruptionEventStore.Workers
}

func getRegionFromQueueURL(queueURL string) string {
	for _, partition := range endpoints.DefaultPartitions() {
		for regionID := range partition.Regions() {
			if strings.Contains(queueURL, regionID) {
				return regionID
			}
		}
	}
	return ""
}
