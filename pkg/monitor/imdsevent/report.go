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

// Package imdsevent moves IMDS events from the node they occur on to the IMDS event controller.
//
// In IMDS event reporting mode, the handler on each node writes the IMDS events of its node to an annotation on its own
// Node object (Reporter) and takes no other action. The IMDS event controller reads the annotations of every node
// (Monitor) and cordons and drains the nodes. The handlers on the nodes then need no permissions on other nodes.
package imdsevent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	corev1 "k8s.io/api/core/v1"
)

const (
	// AnnotationKey is the Node annotation the handler on a node writes its IMDS events to
	AnnotationKey = "aws-node-termination-handler/imds-events"
	// MonitorKind is a const to define this monitor kind
	MonitorKind = "IMDS_EVENT_MONITOR"

	bootIDFile = "/proc/sys/kernel/random/boot_id"
)

// Report is the value of the AnnotationKey annotation
type Report struct {
	// BootID is the boot ID of the node when the report was written. The controller ignores a report whose boot ID is
	// not the node's current boot ID: it was written before the node rebooted.
	BootID string `json:"bootID,omitempty"`
	// ReporterPodUID is the UID of the handler pod that wrote the report. The controller ignores a report whose pod is
	// not running on the node anymore: the DaemonSet was removed from the node, and nothing would update the report.
	ReporterPodUID string           `json:"reporterPodUID"`
	NodeMetadata   ReportedMetadata `json:"nodeMetadata"`
	Events         []Event          `json:"events"`
}

// ReportedMetadata is the instance metadata that the Node object doesn't have. The controller takes the rest of the
// node's metadata, such as its instance ID and addresses, from the Node object, where the node can't change it.
type ReportedMetadata struct {
	AccountID         string `json:"accountId,omitempty"`
	InstanceLifeCycle string `json:"instanceLifeCycle,omitempty"`
}

// NodeMetadata returns the metadata of the node n: its instance ID from the provider ID, its instance type, zone and
// region from the well-known labels and its addresses from the status, which the cloud controller manager sets, and
// from the report only what the Node object doesn't have.
func NodeMetadata(n *corev1.Node, reported ReportedMetadata) ec2metadata.NodeMetadata {
	reported = validMetadata(reported)
	nodeMetadata := ec2metadata.NodeMetadata{
		AccountId:         reported.AccountID,
		InstanceID:        instanceIDFromProviderID(n.Spec.ProviderID),
		InstanceLifeCycle: reported.InstanceLifeCycle,
		InstanceType:      n.Labels[corev1.LabelInstanceTypeStable],
		AvailabilityZone:  n.Labels[corev1.LabelTopologyZone],
		Region:            n.Labels[corev1.LabelTopologyRegion],
	}
	// if there's more than one address of a type, use the first one
	for _, address := range n.Status.Addresses {
		var field *string
		switch address.Type {
		case corev1.NodeInternalIP:
			field = &nodeMetadata.LocalIP
		case corev1.NodeExternalIP:
			field = &nodeMetadata.PublicIP
		case corev1.NodeInternalDNS:
			field = &nodeMetadata.LocalHostname
		case corev1.NodeExternalDNS:
			field = &nodeMetadata.PublicHostname
		default:
			continue
		}
		if *field == "" {
			*field = address.Address
		}
	}
	return nodeMetadata
}

// NodeMetadataFromNode returns the metadata of the node n like NodeMetadata, with what its handler reported in its
// AnnotationKey annotation, if any
func NodeMetadataFromNode(n *corev1.Node) ec2metadata.NodeMetadata {
	var reported ReportedMetadata
	if value, ok := n.Annotations[AnnotationKey]; ok {
		if report, err := Decode(value); err == nil {
			reported = report.NodeMetadata
		}
	}
	return NodeMetadata(n, reported)
}

// Event is an IMDS event of the node. It carries no node name or provider ID: the controller takes them from the Node
// object the annotation is on, so a node can only report events for itself. It carries no description either: the
// controller checks every field (validEvent) and generates the description from them, so no text the node wrote
// reaches a webhook, a Kubernetes event or a log.
type Event struct {
	EventID   string    `json:"eventID"`
	Kind      string    `json:"kind"`
	Monitor   string    `json:"monitor"`
	State     string    `json:"state,omitempty"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime,omitzero"`
	// Code and NotBefore are the code and the start of the window of a scheduled event
	Code      string    `json:"code,omitempty"`
	NotBefore time.Time `json:"notBefore,omitzero"`
	// UncordonAfterReboot is set for scheduled reboot events: the node is uncordoned after it reboots
	UncordonAfterReboot bool `json:"uncordonAfterReboot,omitempty"`
	// Cancelled is set when IMDS reports the event as canceled or completed
	Cancelled bool `json:"cancelled,omitempty"`
}

// Encode returns the annotation value of the report, with the events sorted by event ID
func (r Report) Encode() (string, error) {
	sort.Slice(r.Events, func(i, j int) bool { return r.Events[i].EventID < r.Events[j].EventID })
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("encoding IMDS event report: %w", err)
	}
	return string(b), nil
}

// Decode parses an annotation value written by Encode
func Decode(value string) (Report, error) {
	var r Report
	if err := json.Unmarshal([]byte(value), &r); err != nil {
		return Report{}, fmt.Errorf("decoding IMDS event report: %w", err)
	}
	return r, nil
}

// EventFromInterruptionEvent converts an IMDS interruption event to its reported form
func EventFromInterruptionEvent(e monitor.InterruptionEvent, cancelled bool) Event {
	return Event{
		EventID:             e.EventID,
		Kind:                e.Kind,
		Monitor:             e.Monitor,
		State:               e.State,
		StartTime:           e.StartTime,
		EndTime:             e.EndTime,
		Code:                e.ScheduledEventCode,
		NotBefore:           e.ScheduledEventNotBefore,
		UncordonAfterReboot: e.Kind == monitor.ScheduledEventKind && e.PreDrainTask != nil && !cancelled,
		Cancelled:           cancelled,
	}
}

// ReadBootID returns the boot ID of the system, or "" when it is not available
func ReadBootID() string {
	b, err := os.ReadFile(bootIDFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// controllerEventID returns the event ID the controller uses for an event of a node. Except for scheduled events, IMDS
// event IDs are hashes of the IMDS response, so two nodes interrupted at the same time report the same ID; the
// controller adds a suffix derived from the node name to keep them apart.
func controllerEventID(nodeName string, e Event) string {
	if e.Kind == monitor.ScheduledEventKind {
		return e.EventID
	}
	sum := sha256.Sum256([]byte(nodeName))
	return fmt.Sprintf("%s-%x", e.EventID, sum[:4])
}
