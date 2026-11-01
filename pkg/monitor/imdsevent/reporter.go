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
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Reporter writes the IMDS events of the node it runs on to the AnnotationKey annotation of that node. It only ever
// patches that one annotation of its own node, which is all the ValidatingAdmissionPolicy in the Helm chart allows.
type Reporter struct {
	client       kubernetes.Interface
	nodeName     string
	nodeMetadata ReportedMetadata
	bootID       string
	podUID       string

	mu      sync.Mutex
	events  map[string]Event
	written string
}

// NewReporter returns a Reporter for the node nodeName, running in the pod podUID. It reports only the metadata that the
// Node object doesn't have.
func NewReporter(client kubernetes.Interface, nodeName string, nodeMetadata ec2metadata.NodeMetadata, bootID string, podUID string) *Reporter {
	return &Reporter{
		client:   client,
		nodeName: nodeName,
		nodeMetadata: ReportedMetadata{
			AccountID:         nodeMetadata.AccountId,
			InstanceLifeCycle: nodeMetadata.InstanceLifeCycle,
		},
		bootID: bootID,
		podUID: podUID,
		events: map[string]Event{},
	}
}

// Reset replaces the report of the node with an empty one, removing the events reported before the handler started
func (r *Reporter) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = map[string]Event{}
	r.written = ""
	return r.write()
}

// Report adds an active event to the report. Events already in the report are not written again.
func (r *Reporter) Report(e monitor.InterruptionEvent) error {
	return r.add(EventFromInterruptionEvent(e, false))
}

// ReportCancel marks an event of the report as canceled, adding it when it is not in the report yet
func (r *Reporter) ReportCancel(e monitor.InterruptionEvent) error {
	return r.add(EventFromInterruptionEvent(e, true))
}

func (r *Reporter) add(e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Kind != monitor.ScheduledEventKind {
		// spot interruption, rebalance recommendation and ASG target lifecycle notices end the instance, and their event
		// IDs are hashes of the IMDS response: a response that changes between polls would add an event on every poll.
		// The first event of each of these kinds is the only one reported.
		for id, existing := range r.events {
			if existing.Kind == e.Kind && id != e.EventID {
				return r.write()
			}
		}
	}
	// the monitors report their events on every poll, some with a new start time; the first report of an event is kept,
	// and a canceled event stays canceled
	if existing, ok := r.events[e.EventID]; !ok || (!existing.Cancelled && e.Cancelled) {
		r.events[e.EventID] = e
	}
	return r.write()
}

// write patches the annotation when the report changed since the last successful write. It must be called with r.mu
// held. After a failed write the next call retries, since the IMDS monitors report their events on every poll.
func (r *Reporter) write() error {
	report := Report{BootID: r.bootID, ReporterPodUID: r.podUID, NodeMetadata: r.nodeMetadata, Events: make([]Event, 0, len(r.events))}
	for _, e := range r.events {
		report.Events = append(report.Events, e)
	}
	value, err := report.Encode()
	if err != nil {
		return err
	}
	if value == r.written {
		return nil
	}
	patch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{AnnotationKey: value},
		},
	})
	if err != nil {
		return fmt.Errorf("encoding the %s annotation patch: %w", AnnotationKey, err)
	}
	if _, err := r.client.CoreV1().Nodes().Patch(context.TODO(), r.nodeName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("writing the %s annotation of node %s: %w", AnnotationKey, r.nodeName, err)
	}
	r.written = value
	log.Info().Str("node_name", r.nodeName).Int("events", len(report.Events)).Msg("Reported IMDS events to the IMDS event controller")
	return nil
}
