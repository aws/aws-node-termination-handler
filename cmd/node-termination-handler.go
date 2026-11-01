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
	"github.com/aws/aws-node-termination-handler/pkg/monitor/imdsevent"
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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
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
	podUIDIndex             = "uid"
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
	clusterConfig.QPS = float32(nthConfig.KubeApiQps)
	clusterConfig.Burst = nthConfig.KubeApiBurst
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
	// the IMDS event controller handles the IMDS events of other nodes, so it does not read IMDS
	imdsDisabled := nthConfig.EnableSQSTerminationDraining || nthConfig.EnableIMDSEventController
	imdsEventReporting := !imdsDisabled && nthConfig.IMDSEventReporting

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

	recorder, err := observability.InitK8sEventRecorder(nthConfig.EmitKubernetesEvents, nthConfig.NodeName, imdsDisabled, nodeMetadata, nthConfig.KubernetesEventsExtraAnnotations, clientset)
	if err != nil {
		nthConfig.Print()
		log.Fatal().Err(err).Msg("Unable to create Kubernetes event recorder,")
	}
	if nthConfig.EnableIMDSEventController {
		// annotate the events of a node like its own handler would in IMDS mode
		recorder = recorder.WithNodeMetadata(imdsevent.NodeMetadataFromNode)
	}

	nthConfig.Print()

	// with IMDS event reporting, the IMDS event controller uncordons the node after a reboot
	if !imdsDisabled && !imdsEventReporting && nthConfig.EnableScheduledEventDraining {
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
			SqsMsgVisibilityTimeoutSec:    nthConfig.SqsMsgVisibilityTimeoutSec,
		}
		monitoringFns[sqsEvents] = sqsMonitor
	}

	if imdsEventReporting {
		reporter := imdsevent.NewReporter(clientset, nthConfig.NodeName, nodeMetadata, imdsevent.ReadBootID(), nthConfig.PodUID)
		if err := reporter.Reset(); err != nil {
			log.Err(err).Msg("Unable to reset the IMDS event report of this node")
		}
		startMonitors(monitoringFns, nthConfig, metrics, recorder)
		log.Info().Msg("Kubernetes AWS Node Termination Handler has started successfully, reporting IMDS events to the IMDS event controller")
		reportIMDSEvents(reporter, interruptionChan, cancelChan, signalChan)
		log.Info().Msg("AWS Node Termination Handler is shutting down")
		return
	}

	if nthConfig.EnableIMDSEventController {
		runIMDSEventController(nthConfig, clientset, monitoringFns, interruptionChan, cancelChan, interruptionEventStore, node, nodeMetadata, metrics, recorder, signalChan)
		return
	}

	startMonitors(monitoringFns, nthConfig, metrics, recorder)
	runInterruptionLoop(nthConfig, clientset, interruptionChan, cancelChan, interruptionEventStore, node, nodeMetadata, metrics, recorder, signalChan)
}

// startMonitors polls every monitor every 2 seconds
func startMonitors(monitoringFns map[string]monitor.Monitor, nthConfig config.Config, metrics observability.Metrics, recorder observability.K8sEventRecorder) {
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
}

// runInterruptionLoop handles the interruption events of the monitors until SIGTERM
func runInterruptionLoop(nthConfig config.Config, clientset *kubernetes.Clientset, interruptionChan <-chan monitor.InterruptionEvent, cancelChan <-chan monitor.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, node *node.Node, nodeMetadata ec2metadata.NodeMetadata, metrics observability.Metrics, recorder observability.K8sEventRecorder, signalChan <-chan os.Signal) {
	go watchForInterruptionEvents(interruptionChan, interruptionEventStore)
	log.Info().Msg("Started watching for interruption events")
	log.Info().Msg("Kubernetes AWS Node Termination Handler has started successfully!")

	go watchForCancellationEvents(cancelChan, interruptionEventStore, node, metrics, recorder)
	log.Info().Msg("Started watching for event cancellations")

	var wg sync.WaitGroup

	asgLaunchHandler := launch.New(interruptionEventStore, *node, nthConfig, metrics, recorder, clientset)
	drainCordonHander := draincordon.New(interruptionEventStore, *node, nthConfig, nodeMetadata, metrics, recorder)

InterruptionLoop:
	for range time.NewTicker(1 * time.Second).C {
		select {
		case <-signalChan:
			// Exit interruption loop if a SIGTERM is received or the channel is closed
			break InterruptionLoop
		default:
		EventLoop:
			for event, ok := interruptionEventStore.GetActiveEvent(); ok; event, ok = interruptionEventStore.GetActiveEvent() {
				select {
				case interruptionEventStore.Workers <- 1:
					logging.VersionedMsgs.ProcessingInterruptionEvent(event)
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

// reportIMDSEvents writes the events of the IMDS monitors to this node's IMDS event report until SIGTERM
func reportIMDSEvents(reporter *imdsevent.Reporter, interruptionChan <-chan monitor.InterruptionEvent, cancelChan <-chan monitor.InterruptionEvent, signalChan <-chan os.Signal) {
	for {
		select {
		case event := <-interruptionChan:
			if err := reporter.Report(event); err != nil {
				log.Err(err).Str("event_id", event.EventID).Msg("Unable to report an IMDS event to the IMDS event controller")
			}
		case event := <-cancelChan:
			if err := reporter.ReportCancel(event); err != nil {
				log.Err(err).Str("event_id", event.EventID).Msg("Unable to report an IMDS event cancellation to the IMDS event controller")
			}
		case <-signalChan:
			return
		}
	}
}

// runIMDSEventController handles the IMDS events that the handlers on the nodes report, while this replica holds the
// leader election lease, until SIGTERM
func runIMDSEventController(nthConfig config.Config, clientset *kubernetes.Clientset, monitoringFns map[string]monitor.Monitor, interruptionChan chan monitor.InterruptionEvent, cancelChan chan monitor.InterruptionEvent, interruptionEventStore *interruptioneventstore.Store, node *node.Node, nodeMetadata ec2metadata.NodeMetadata, metrics observability.Metrics, recorder observability.K8sEventRecorder, signalChan <-chan os.Signal) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-signalChan
		cancel()
	}()

	identity := nthConfig.PodName
	if identity == "" {
		identity, _ = os.Hostname()
	}
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: nthConfig.IMDSEventControllerLeaseName, Namespace: nthConfig.PodNamespace},
		Client:     clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Info().Str("identity", identity).Msg("Started leading the IMDS event controller")
				factory := informers.NewSharedInformerFactory(clientset, 0)
				nodeLister := factory.Core().V1().Nodes().Lister()
				// the handler pods on the nodes run in the controller's namespace
				podFactory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithNamespace(nthConfig.PodNamespace))
				podInformer := podFactory.Core().V1().Pods().Informer()
				if err := podInformer.AddIndexers(cache.Indexers{podUIDIndex: func(obj interface{}) ([]string, error) {
					return []string{string(obj.(*corev1.Pod).UID)}, nil
				}}); err != nil {
					log.Fatal().Err(err).Msg("Unable to index pods by UID")
				}
				factory.Start(ctx.Done())
				podFactory.Start(ctx.Done())
				factory.WaitForCacheSync(ctx.Done())
				podFactory.WaitForCacheSync(ctx.Done())
				reporterRunning := func(nodeName string, podUID string) bool {
					if podUID == "" {
						return false
					}
					pods, err := podInformer.GetIndexer().ByIndex(podUIDIndex, podUID)
					if err != nil || len(pods) != 1 {
						return false
					}
					pod := pods[0].(*corev1.Pod)
					return pod.Spec.NodeName == nodeName && pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil
				}

				rebootHandler := func(nodeName string) (string, error) {
					eventID, err := node.GetEventID(nodeName)
					if err != nil {
						return "", err
					}
					uncordoned, err := node.UncordonIfRebootedResult(nodeName)
					if err != nil || !uncordoned {
						return "", err
					}
					return eventID, nil
				}
				monitoringFns[imdsevent.MonitorKind] = imdsevent.NewMonitor(nodeLister, interruptionChan, cancelChan, rebootHandler, interruptionEventStore.IgnoreEvent, reporterRunning)
				startMonitors(monitoringFns, nthConfig, metrics, recorder)

				loopSignal := make(chan os.Signal, 1)
				go func() {
					<-ctx.Done()
					loopSignal <- syscall.SIGTERM
				}()
				runInterruptionLoop(nthConfig, clientset, interruptionChan, cancelChan, interruptionEventStore, node, nodeMetadata, metrics, recorder, loopSignal)
			},
			OnStoppedLeading: func() {
				if ctx.Err() != nil {
					log.Info().Str("identity", identity).Msg("Stopped leading the IMDS event controller")
					return
				}
				// the monitors cannot be stopped, so exit and let the Deployment restart this replica
				log.Fatal().Str("identity", identity).Msg("Lost the IMDS event controller leader election lease")
			},
		},
	})
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
		return fmt.Errorf("unable to complete node label actions: %w", err)
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
		// The IMDS monitors report a canceled event on every poll. A cancellation uncordons the node only when NTH acted
		// on the event, so that a node cordoned for another reason stays cordoned: the event is in the event store, or,
		// when NTH restarted since it acted, the node is still labeled with the event ID.
		inStore := interruptionEventStore.CancelInterruptionEvent(eventID)
		if !inStore {
			labeled, err := node.HasEventID(nodeName, eventID)
			if err != nil {
				log.Warn().Err(err).Str("event_id", eventID).Msg("Unable to check whether the node is labeled with the canceled event")
				continue
			}
			if !labeled {
				log.Debug().Str("event_id", eventID).Msg("Ignoring the cancellation of an event NTH did not act on")
				continue
			}
		}
		if (inStore && interruptionEventStore.ShouldUncordonNode(nodeName)) || (!inStore && !interruptionEventStore.HasActiveEvent(nodeName)) {
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
