// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package imdsevent

import (
	"fmt"
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/asglifecycle"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/rebalancerecommendation"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corelisters "k8s.io/client-go/listers/core/v1"
)

// ReporterRunning reports whether the handler pod podUID is running on the node nodeName
type ReporterRunning func(nodeName string, podUID string) bool

// RebootHandler uncordons a node marked for uncordon after reboot once it has rebooted, and returns the ID of the event
// that caused the reboot, or "" when the node was not uncordoned
type RebootHandler func(nodeName string) (eventID string, err error)

// Monitor is the IMDS event controller's monitor: it reads the IMDS event reports of every node and sends their events
// to the interruption and cancellation channels, like the IMDS monitors do on the node itself.
type Monitor struct {
	nodes            corelisters.NodeLister
	interruptionChan chan<- monitor.InterruptionEvent
	cancelChan       chan<- monitor.InterruptionEvent
	rebootHandler    RebootHandler
	ignoreEvent      func(eventID string)
	reporterRunning  ReporterRunning

	// cancelled holds the controller event IDs whose cancellation was sent. Whether a cancellation uncordons the node is
	// decided by the cancellation handler, from the event ID label NTH sets on the node when it acts on the event.
	cancelled map[string]struct{}
}

// NewMonitor returns the IMDS event controller's monitor
func NewMonitor(nodes corelisters.NodeLister, interruptionChan chan<- monitor.InterruptionEvent, cancelChan chan<- monitor.InterruptionEvent, rebootHandler RebootHandler, ignoreEvent func(eventID string), reporterRunning ReporterRunning) *Monitor {
	return &Monitor{
		reporterRunning:  reporterRunning,
		nodes:            nodes,
		interruptionChan: interruptionChan,
		cancelChan:       cancelChan,
		rebootHandler:    rebootHandler,
		ignoreEvent:      ignoreEvent,
		cancelled:        map[string]struct{}{},
	}
}

// Kind denotes the kind of monitor
func (m *Monitor) Kind() string {
	return MonitorKind
}

// Monitor reads the reports of every node once
func (m *Monitor) Monitor() error {
	nodes, err := m.nodes.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}
	for _, n := range nodes {
		m.handleReboot(n)
		m.handleReport(n)
	}
	return nil
}

func (m *Monitor) handleReboot(n *corev1.Node) {
	if n.Labels[node.ActionLabelKey] != node.UncordonAfterRebootLabelVal {
		return
	}
	eventID, err := m.rebootHandler(n.Name)
	if err != nil {
		log.Warn().Err(err).Str("node_name", n.Name).Msg("Unable to complete the uncordon after reboot workflow")
		return
	}
	if eventID != "" {
		// the event that caused the reboot can still be reported after the reboot
		m.ignoreEvent(eventID)
	}
}

func (m *Monitor) handleReport(n *corev1.Node) {
	value, ok := n.Annotations[AnnotationKey]
	if !ok {
		return
	}
	report, err := Decode(value)
	if err != nil {
		log.Warn().Err(err).Str("node_name", n.Name).Msgf("Ignoring the %s annotation", AnnotationKey)
		return
	}
	if !m.reporterRunning(n.Name, report.ReporterPodUID) {
		log.Debug().Str("node_name", n.Name).Str("reporter_pod_uid", report.ReporterPodUID).Msg("Ignoring IMDS events reported by a handler pod that is not running on the node")
		return
	}
	if report.BootID != "" && n.Status.NodeInfo.BootID != "" && report.BootID != n.Status.NodeInfo.BootID {
		log.Debug().Str("node_name", n.Name).Msg("Ignoring IMDS events reported before the node rebooted")
		return
	}
	for _, e := range report.Events {
		e, description, err := validEvent(e)
		if err != nil {
			log.Warn().Err(err).Str("node_name", n.Name).Msgf("Ignoring an invalid event in the %s annotation", AnnotationKey)
			continue
		}
		event := m.interruptionEvent(n, report, e)
		event.Description = description
		if e.Cancelled {
			if _, sent := m.cancelled[event.EventID]; sent {
				continue
			}
			m.cancelled[event.EventID] = struct{}{}
			m.cancelChan <- event
			continue
		}
		m.interruptionChan <- event
	}
}

// interruptionEvent converts a reported event to an interruption event. The node name, provider ID and all of the
// node's metadata the Node object has come from the Node object, not from the report.
func (m *Monitor) interruptionEvent(n *corev1.Node, report Report, e Event) monitor.InterruptionEvent {
	nodeMetadata := NodeMetadata(n, report.NodeMetadata)
	return monitor.InterruptionEvent{
		EventID:      controllerEventID(n.Name, e),
		Kind:         e.Kind,
		Monitor:      e.Monitor,
		State:        e.State,
		NodeName:     n.Name,
		ProviderID:   n.Spec.ProviderID,
		InstanceID:   nodeMetadata.InstanceID,
		InstanceType: nodeMetadata.InstanceType,
		IsManaged:    true,
		StartTime:    e.StartTime,
		EndTime:      e.EndTime,
		NodeMetadata: &nodeMetadata,
		PreDrainTask: preDrainTask(e),
	}
}

// preDrainTask returns the pre-drain task the IMDS monitor on the node sets for the event
func preDrainTask(e Event) monitor.DrainTask {
	switch e.Kind {
	case monitor.SpotITNKind:
		return spotitn.PreDrainTask
	case monitor.RebalanceRecommendationKind:
		return rebalancerecommendation.PreDrainTask
	case monitor.ASGLifecycleKind:
		return asglifecycle.PreDrainTask
	case monitor.ScheduledEventKind:
		if e.UncordonAfterReboot {
			return scheduledevent.UncordonAfterRebootPreDrain
		}
	}
	return nil
}

// instanceIDFromProviderID returns the instance ID of an AWS provider ID (aws:///us-west-2a/i-0123456789abcdef0), or ""
func instanceIDFromProviderID(providerID string) string {
	if !strings.HasPrefix(providerID, "aws://") {
		return ""
	}
	id := providerID[strings.LastIndex(providerID, "/")+1:]
	if !strings.HasPrefix(id, "i-") {
		return ""
	}
	return id
}
