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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubectl/pkg/drain"
)

// The scheduled event monitor reports a canceled event on every poll. Only the first cancellation of an event in the
// store may uncordon the node: later ones must not undo a cordon set for another reason.
func TestWatchForCancellationEventsUncordonsOnce(t *testing.T) {
	const nodeName = "node-1"
	client := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}, Spec: v1.NodeSpec{Unschedulable: true}})
	nthConfig := config.Config{NodeName: nodeName}
	n, err := node.NewWithValues(nthConfig, &drain.Helper{Ctx: context.TODO(), Client: client}, uptime.Uptime)
	h.Ok(t, err)
	store := interruptioneventstore.New(nthConfig)
	event := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
	store.AddInterruptionEvent(&event)

	cancelChan := make(chan monitor.InterruptionEvent)
	go watchForCancellationEvents(cancelChan, store, n, observability.Metrics{}, observability.K8sEventRecorder{})

	unschedulable := func() bool {
		got, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		h.Ok(t, err)
		return got.Spec.Unschedulable
	}

	cancelChan <- event
	h.Ok(t, waitFor(func() bool { return !unschedulable() }))

	// an administrator cordons the node, and IMDS still reports the canceled event
	_, err = client.CoreV1().Nodes().Patch(context.TODO(), nodeName, "application/merge-patch+json", []byte(`{"spec":{"unschedulable":true}}`), metav1.PatchOptions{})
	h.Ok(t, err)
	cancelChan <- event
	cancelChan <- event // the previous cancellation is handled once this send returns
	h.Equals(t, true, unschedulable())
}

// After NTH restarts, the event store is empty. A cancellation still uncordons the node when the node is labeled with the
// event ID, and only then.
func TestWatchForCancellationEventsAfterRestart(t *testing.T) {
	const nodeName = "node-1"
	for _, tc := range []struct {
		name       string
		label      string
		otherEvent bool
		uncordoned bool
	}{
		{name: "labeled with the canceled event", label: "instance-event-1", uncordoned: true},
		{name: "not labeled", label: "", uncordoned: false},
		{name: "labeled with another event", label: "instance-event-2", uncordoned: false},
		{name: "another event is active", label: "instance-event-1", otherEvent: true, uncordoned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labels := map[string]string{}
			if tc.label != "" {
				labels[node.EventIDLabelKey] = tc.label
			}
			client := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: labels}, Spec: v1.NodeSpec{Unschedulable: true}})
			nthConfig := config.Config{NodeName: nodeName}
			n, err := node.NewWithValues(nthConfig, &drain.Helper{Ctx: context.TODO(), Client: client}, uptime.Uptime)
			h.Ok(t, err)
			store := interruptioneventstore.New(nthConfig)
			if tc.otherEvent {
				store.AddInterruptionEvent(&monitor.InterruptionEvent{EventID: "spot-itn-1", Kind: monitor.SpotITNKind, NodeName: nodeName, StartTime: time.Now()})
			}

			cancelChan := make(chan monitor.InterruptionEvent)
			go watchForCancellationEvents(cancelChan, store, n, observability.Metrics{}, observability.K8sEventRecorder{})
			event := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
			cancelChan <- event
			cancelChan <- event // the previous cancellation is handled once this send returns

			got, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
			h.Ok(t, err)
			h.Equals(t, !tc.uncordoned, got.Spec.Unschedulable)
		})
	}
}

func waitFor(cond func() bool) error {
	for i := 0; i < 100; i++ {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
