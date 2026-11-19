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
	"errors"
	"fmt"
	"reflect"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

// A decided cancellation is not looked up again when IMDS reports it on later polls
func TestHandleCancellationEventLooksUpNodeOnce(t *testing.T) {
	const nodeName = "node-1"
	for _, tc := range []struct {
		name       string
		label      string
		inStore    bool
		uncordoned bool
	}{
		{name: "in the store", inStore: true, uncordoned: true},
		{name: "labeled with the canceled event", label: "instance-event-1", uncordoned: true},
		{name: "not labeled", label: "", uncordoned: false},
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
			event := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
			if tc.inStore {
				store.AddInterruptionEvent(&event)
			}
			handled := map[string]struct{}{}

			handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
			got, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
			h.Ok(t, err)
			h.Equals(t, !tc.uncordoned, got.Spec.Unschedulable)

			client.ClearActions()
			for i := 0; i < 5; i++ {
				handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
			}
			h.Equals(t, 0, len(client.Actions()))
		})
	}
}

// A cancellation whose node lookup failed is looked up again on its next report
func TestHandleCancellationEventRetriesLookupError(t *testing.T) {
	const nodeName = "node-1"
	client := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{node.EventIDLabelKey: "instance-event-1"}}, Spec: v1.NodeSpec{Unschedulable: true}})
	failing := true
	failNodeReads := func(action k8stesting.Action) (bool, runtime.Object, error) {
		return failing, nil, errors.New("apiserver unavailable")
	}
	client.PrependReactor("list", "nodes", failNodeReads)
	client.PrependReactor("get", "nodes", failNodeReads)
	nthConfig := config.Config{NodeName: nodeName}
	n, err := node.NewWithValues(nthConfig, &drain.Helper{Ctx: context.TODO(), Client: client}, uptime.Uptime)
	h.Ok(t, err)
	store := interruptioneventstore.New(nthConfig)
	event := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
	handled := map[string]struct{}{}

	handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
	h.Equals(t, 0, len(handled))

	failing = false
	client.ClearActions()
	handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
	h.Assert(t, countNodeReads(client) > 0, "expected the node to be looked up again")
	got, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	h.Ok(t, err)
	h.Equals(t, false, got.Spec.Unschedulable)
	h.Equals(t, 1, len(handled))
}

// A cancellation reported while another event for the node is active is decided once no other event is active
func TestHandleCancellationEventWaitsForOtherEvent(t *testing.T) {
	const nodeName = "node-1"
	client := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{node.EventIDLabelKey: "instance-event-1"}}, Spec: v1.NodeSpec{Unschedulable: true}})
	nthConfig := config.Config{NodeName: nodeName}
	n, err := node.NewWithValues(nthConfig, &drain.Helper{Ctx: context.TODO(), Client: client}, uptime.Uptime)
	h.Ok(t, err)
	store := interruptioneventstore.New(nthConfig)
	store.AddInterruptionEvent(&monitor.InterruptionEvent{EventID: "spot-itn-1", Kind: monitor.SpotITNKind, NodeName: nodeName, StartTime: time.Now()})
	event := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
	handled := map[string]struct{}{}

	handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
	h.Equals(t, 0, len(client.Actions()))
	h.Equals(t, 0, len(handled))

	store.CancelInterruptionEvent("spot-itn-1")
	handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
	got, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	h.Ok(t, err)
	h.Equals(t, false, got.Spec.Unschedulable)
	h.Equals(t, 1, len(handled))
}

// The decided cancellation event IDs are bounded
func TestHandleCancellationEventBound(t *testing.T) {
	const nodeName = "node-1"
	client := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
	nthConfig := config.Config{NodeName: nodeName}
	n, err := node.NewWithValues(nthConfig, &drain.Helper{Ctx: context.TODO(), Client: client}, uptime.Uptime)
	h.Ok(t, err)
	store := interruptioneventstore.New(nthConfig)
	handled := map[string]struct{}{}

	for i := 0; i <= maxHandledCancellations; i++ {
		event := monitor.InterruptionEvent{EventID: fmt.Sprintf("instance-event-%d", i), Kind: monitor.ScheduledEventKind, NodeName: nodeName, StartTime: time.Now()}
		handleCancellationEvent(event, store, n, observability.Metrics{}, observability.K8sEventRecorder{}, handled)
		h.Assert(t, len(handled) <= maxHandledCancellations, "handled holds %d event IDs", len(handled))
	}
	_, ok := handled[fmt.Sprintf("instance-event-%d", maxHandledCancellations)]
	h.Assert(t, ok, "expected the last event ID to be recorded")
}

func countNodeReads(client *fake.Clientset) int {
	count := 0
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "nodes" && (action.GetVerb() == "list" || action.GetVerb() == "get") {
			count++
		}
	}
	return count
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

func TestCachesSynced(t *testing.T) {
	nodes := reflect.TypeOf(&v1.Node{})
	pods := reflect.TypeOf(&v1.Pod{})
	h.Equals(t, true, cachesSynced(map[reflect.Type]bool{nodes: true}, map[reflect.Type]bool{pods: true}))
	h.Equals(t, true, cachesSynced())
	h.Equals(t, false, cachesSynced(map[reflect.Type]bool{nodes: true}, map[reflect.Type]bool{pods: false}))
	h.Equals(t, false, cachesSynced(map[reflect.Type]bool{nodes: false, pods: true}))
}
