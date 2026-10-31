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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

const (
	testNode   = "ip-10-0-0-1.ec2.internal"
	otherNode  = "ip-10-0-0-2.ec2.internal"
	testBootID = "boot-1"
	testPodUID = "pod-uid-1"
)

var startTime = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// spotID is a spot interruption event ID of the form the IMDS monitor writes
var spotID = "spot-itn-" + strings.Repeat("ab", 32)

func spotEvent(id string) monitor.InterruptionEvent {
	return monitor.InterruptionEvent{EventID: id, Kind: monitor.SpotITNKind, Monitor: "SPOT_ITN_MONITOR", StartTime: startTime, NodeName: testNode}
}

func newFakeNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			corev1.LabelInstanceTypeStable: "m5.large",
			corev1.LabelTopologyZone:       "us-west-2a",
			corev1.LabelTopologyRegion:     "us-west-2",
		}},
		Spec: corev1.NodeSpec{ProviderID: "aws:///us-west-2a/i-0123456789abcdef0"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{BootID: testBootID},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
				{Type: corev1.NodeInternalDNS, Address: "ip-10-0-0-1.us-west-2.compute.internal"},
				{Type: corev1.NodeHostName, Address: name},
			},
		},
	}
}

func reportOf(t *testing.T, client *fake.Clientset, name string) Report {
	t.Helper()
	n, err := client.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	h.Ok(t, err)
	value, ok := n.Annotations[AnnotationKey]
	h.Assert(t, ok, "node %s has no %s annotation", name, AnnotationKey)
	r, err := Decode(value)
	h.Ok(t, err)
	return r
}

func countPatches(client *fake.Clientset) int {
	count := 0
	for _, a := range client.Actions() {
		if a.GetVerb() == "patch" {
			count++
		}
	}
	return count
}

func TestReporterWritesOnlyItsOwnNodeAndOnlyOnChange(t *testing.T) {
	client := fake.NewSimpleClientset(newFakeNode(testNode), newFakeNode(otherNode))
	r := NewReporter(client, testNode, ec2metadata.NodeMetadata{AccountId: "123456789012", InstanceID: "i-0123456789abcdef0", InstanceLifeCycle: "spot", LocalIP: "10.0.0.1"}, testBootID, testPodUID)

	h.Ok(t, r.Reset())
	h.Equals(t, 0, len(reportOf(t, client, testNode).Events))

	h.Ok(t, r.Report(spotEvent("spot-itn-1")))
	h.Ok(t, r.Report(spotEvent("spot-itn-1")))
	h.Equals(t, 2, countPatches(client))

	report := reportOf(t, client, testNode)
	h.Equals(t, testBootID, report.BootID)
	h.Equals(t, testPodUID, report.ReporterPodUID)
	// only the metadata the Node object doesn't have
	h.Equals(t, ReportedMetadata{AccountID: "123456789012", InstanceLifeCycle: "spot"}, report.NodeMetadata)
	h.Equals(t, []Event{{EventID: "spot-itn-1", Kind: monitor.SpotITNKind, Monitor: "SPOT_ITN_MONITOR", StartTime: startTime}}, report.Events)

	for _, a := range client.Actions() {
		if patch, ok := a.(k8stesting.PatchAction); ok {
			h.Equals(t, testNode, patch.GetName())
		}
	}
	other, err := client.CoreV1().Nodes().Get(context.TODO(), otherNode, metav1.GetOptions{})
	h.Ok(t, err)
	_, ok := other.Annotations[AnnotationKey]
	h.Assert(t, !ok, "the other node must not be annotated")
}

func TestReporterRetriesAfterFailedWrite(t *testing.T) {
	client := fake.NewSimpleClientset(newFakeNode(testNode))
	failures := 1
	client.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failures > 0 {
			failures--
			return true, nil, errors.New("denied")
		}
		return false, nil, nil
	})
	r := NewReporter(client, testNode, ec2metadata.NodeMetadata{}, testBootID, testPodUID)
	h.Nok(t, r.Report(spotEvent("spot-itn-1")))
	h.Ok(t, r.Report(spotEvent("spot-itn-1")))
	h.Equals(t, 1, len(reportOf(t, client, testNode).Events))
}

func TestReporterKeepsTheFirstReportOfAnEventAndKeepsCancellations(t *testing.T) {
	client := fake.NewSimpleClientset(newFakeNode(testNode))
	r := NewReporter(client, testNode, ec2metadata.NodeMetadata{}, testBootID, testPodUID)
	asg := monitor.InterruptionEvent{EventID: "target-lifecycle-state-terminated-1", Kind: monitor.ASGLifecycleKind, StartTime: startTime}
	h.Ok(t, r.Report(asg))
	asg.StartTime = startTime.Add(2 * time.Second)
	h.Ok(t, r.Report(asg))
	scheduled := monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, PreDrainTask: func(monitor.InterruptionEvent, node.Node) error { return nil }}
	h.Ok(t, r.Report(scheduled))
	h.Ok(t, r.ReportCancel(scheduled))
	h.Ok(t, r.Report(scheduled))
	h.Ok(t, r.ReportCancel(scheduled))
	h.Equals(t, 3, countPatches(client))

	events := reportOf(t, client, testNode).Events
	h.Equals(t, 2, len(events))
	h.Equals(t, "instance-event-1", events[0].EventID)
	h.Assert(t, events[0].Cancelled && !events[0].UncordonAfterReboot, "a canceled event must stay canceled: %+v", events[0])
	h.Equals(t, startTime, events[1].StartTime)
}

func TestReporterKeepsOneEventPerTerminalKind(t *testing.T) {
	client := fake.NewSimpleClientset(newFakeNode(testNode))
	r := NewReporter(client, testNode, ec2metadata.NodeMetadata{}, testBootID, testPodUID)
	// a mock IMDS that returns a new interruption time on every request
	for i := 0; i < 5; i++ {
		h.Ok(t, r.Report(spotEvent(fmt.Sprintf("spot-itn-%d", i))))
	}
	h.Ok(t, r.Report(monitor.InterruptionEvent{EventID: "rebalance-recommendation-1", Kind: monitor.RebalanceRecommendationKind}))
	h.Ok(t, r.Report(monitor.InterruptionEvent{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind}))
	h.Ok(t, r.Report(monitor.InterruptionEvent{EventID: "instance-event-2", Kind: monitor.ScheduledEventKind}))

	var ids []string
	for _, e := range reportOf(t, client, testNode).Events {
		ids = append(ids, e.EventID)
	}
	h.Equals(t, []string{"instance-event-1", "instance-event-2", "rebalance-recommendation-1", "spot-itn-0"}, ids)
}

type monitorHarness struct {
	indexer      cache.Indexer
	monitor      *Monitor
	interruption chan monitor.InterruptionEvent
	cancel       chan monitor.InterruptionEvent
	ignored      []string
	rebooted     map[string]string
	running      map[string]string // node name -> running handler pod UID
}

func newMonitorHarness() *monitorHarness {
	mh := &monitorHarness{
		indexer:      cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		interruption: make(chan monitor.InterruptionEvent, 10),
		cancel:       make(chan monitor.InterruptionEvent, 10),
		rebooted:     map[string]string{},
		running:      map[string]string{testNode: testPodUID, otherNode: testPodUID},
	}
	rebootHandler := func(nodeName string) (string, error) { return mh.rebooted[nodeName], nil }
	reporterRunning := func(nodeName, podUID string) bool { return podUID != "" && mh.running[nodeName] == podUID }
	mh.monitor = NewMonitor(corelisters.NewNodeLister(mh.indexer), mh.interruption, mh.cancel, rebootHandler, func(id string) { mh.ignored = append(mh.ignored, id) }, reporterRunning)
	return mh
}

func (mh *monitorHarness) setReport(t *testing.T, n *corev1.Node, r Report) {
	t.Helper()
	if r.ReporterPodUID == "" {
		r.ReporterPodUID = testPodUID
	}
	value, err := r.Encode()
	h.Ok(t, err)
	n.Annotations = map[string]string{AnnotationKey: value}
	h.Ok(t, mh.indexer.Update(n))
}

func drain(c chan monitor.InterruptionEvent) []monitor.InterruptionEvent {
	var out []monitor.InterruptionEvent
	for {
		select {
		case e := <-c:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestMonitorTakesNodeIdentityFromTheNodeObject(t *testing.T) {
	mh := newMonitorHarness()
	n := newFakeNode(testNode)
	mh.setReport(t, n, Report{BootID: testBootID, NodeMetadata: ReportedMetadata{AccountID: "123456789012", InstanceLifeCycle: "spot"}, Events: []Event{{EventID: spotID, Kind: monitor.SpotITNKind, StartTime: startTime}}})
	h.Ok(t, mh.monitor.Monitor())

	events := drain(mh.interruption)
	h.Equals(t, 1, len(events))
	e := events[0]
	h.Equals(t, testNode, e.NodeName)
	h.Equals(t, "aws:///us-west-2a/i-0123456789abcdef0", e.ProviderID)
	h.Equals(t, "i-0123456789abcdef0", e.InstanceID)
	h.Equals(t, "m5.large", e.InstanceType)
	h.Equals(t, ec2metadata.NodeMetadata{
		AccountId:         "123456789012",
		InstanceID:        "i-0123456789abcdef0",
		InstanceLifeCycle: "spot",
		InstanceType:      "m5.large",
		LocalHostname:     "ip-10-0-0-1.us-west-2.compute.internal",
		LocalIP:           "10.0.0.1",
		AvailabilityZone:  "us-west-2a",
		Region:            "us-west-2",
	}, *e.NodeMetadata)
	h.Assert(t, e.PreDrainTask != nil, "spot events must get the spot pre-drain task")
}

func TestNodeMetadataFromNodeWithoutReport(t *testing.T) {
	nodeMetadata := NodeMetadataFromNode(newFakeNode(testNode))
	h.Equals(t, "i-0123456789abcdef0", nodeMetadata.InstanceID)
	h.Equals(t, "10.0.0.1", nodeMetadata.LocalIP)
	h.Equals(t, "", nodeMetadata.InstanceLifeCycle)
}

func TestMonitorKeepsIdenticalEventIDsOfTwoNodesApart(t *testing.T) {
	mh := newMonitorHarness()
	for _, name := range []string{testNode, otherNode} {
		mh.setReport(t, newFakeNode(name), Report{Events: []Event{{EventID: spotID, Kind: monitor.SpotITNKind}}})
	}
	h.Ok(t, mh.monitor.Monitor())
	events := drain(mh.interruption)
	h.Equals(t, 2, len(events))
	h.Assert(t, events[0].EventID != events[1].EventID, "event IDs must differ: %s", events[0].EventID)
}

func TestMonitorIgnoresReportsOfHandlerPodsThatAreNotRunning(t *testing.T) {
	mh := newMonitorHarness()
	// the DaemonSet no longer runs on the node: the report of its last pod stays on the Node object
	mh.setReport(t, newFakeNode(testNode), Report{ReporterPodUID: "pod-uid-gone", Events: []Event{{EventID: spotID, Kind: monitor.SpotITNKind}}})
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, 0, len(drain(mh.interruption)))

	mh.running[testNode] = "pod-uid-gone"
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, 1, len(drain(mh.interruption)))
}

func TestMonitorIgnoresReportsFromBeforeAReboot(t *testing.T) {
	mh := newMonitorHarness()
	mh.setReport(t, newFakeNode(testNode), Report{BootID: "boot-0", Events: []Event{{EventID: spotID, Kind: monitor.SpotITNKind}}})
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, 0, len(drain(mh.interruption)))
}

// A new leader has not seen the event active, so the monitor forwards every cancellation once; the cancellation handler
// decides from the node's event ID label whether NTH acted on the event.
func TestMonitorSendsEachCancellationOnce(t *testing.T) {
	mh := newMonitorHarness()
	n := newFakeNode(testNode)
	mh.setReport(t, n, Report{Events: []Event{{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, Code: "system-reboot", UncordonAfterReboot: true}}})
	h.Ok(t, mh.monitor.Monitor())
	active := drain(mh.interruption)
	h.Equals(t, 1, len(active))
	h.Equals(t, "instance-event-1", active[0].EventID)
	h.Assert(t, active[0].PreDrainTask != nil, "scheduled reboot events must get the uncordon after reboot pre-drain task")

	mh.setReport(t, n, Report{Events: []Event{{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, Code: "system-reboot", Cancelled: true}}})
	h.Ok(t, mh.monitor.Monitor())
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, 1, len(drain(mh.cancel)))

	// a monitor that never saw the event active, as after a leader change, still forwards its cancellation
	restarted := newMonitorHarness()
	restarted.setReport(t, n, Report{Events: []Event{{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, Code: "system-reboot", Cancelled: true}}})
	h.Ok(t, restarted.monitor.Monitor())
	h.Equals(t, 1, len(drain(restarted.cancel)))
}

func TestMonitorUncordonsRebootedNodesAndIgnoresTheirEvent(t *testing.T) {
	mh := newMonitorHarness()
	n := newFakeNode(testNode)
	n.Labels = map[string]string{node.ActionLabelKey: node.UncordonAfterRebootLabelVal}
	h.Ok(t, mh.indexer.Add(n))
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, 0, len(mh.ignored))

	mh.rebooted[testNode] = "instance-event-1"
	h.Ok(t, mh.monitor.Monitor())
	h.Equals(t, []string{"instance-event-1"}, mh.ignored)
}

func TestInstanceIDFromProviderID(t *testing.T) {
	h.Equals(t, "i-0123456789abcdef0", instanceIDFromProviderID("aws:///us-west-2a/i-0123456789abcdef0"))
	h.Equals(t, "", instanceIDFromProviderID("kind://docker/kind/kind-worker"))
	h.Equals(t, "", instanceIDFromProviderID(""))
}

func TestMonitorGeneratesTheDescriptions(t *testing.T) {
	mh := newMonitorHarness()
	n := newFakeNode(testNode)
	notBefore := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	// the node's text is ignored: the controller decodes no description, and generates its own
	n.Annotations = map[string]string{AnnotationKey: `{"reporterPodUID":"` + testPodUID + `","events":[` +
		`{"eventID":"` + spotID + `","kind":"SPOT_ITN","monitor":"FORGED","description":"\"}<!channel> forged","startTime":"2026-10-30T12:00:00Z"},` +
		`{"eventID":"instance-event-0d59937288b749b32","kind":"SCHEDULED_EVENT","monitor":"SCHEDULED_EVENT_MONITOR","description":"forged","state":"active","code":"system-maintenance","startTime":"2026-10-30T12:00:00Z","notBefore":"` + notBefore.Format(time.RFC3339) + `","endTime":"2026-11-02T10:00:00Z"}]}`}
	h.Ok(t, mh.indexer.Update(n))
	h.Ok(t, mh.monitor.Monitor())

	descriptions := map[string]string{}
	for _, e := range drain(mh.interruption) {
		descriptions[e.Kind] = e.Description
		if e.Kind == monitor.SpotITNKind {
			h.Equals(t, "SPOT_ITN_MONITOR", e.Monitor)
		}
	}
	h.Equals(t, map[string]string{
		monitor.SpotITNKind:        "Spot ITN received. Instance will be interrupted at 2026-10-30T12:00:00Z \n",
		monitor.ScheduledEventKind: "system-maintenance will occur between 2 Nov 2026 08:00:00 GMT and 2 Nov 2026 10:00:00 GMT\n",
	}, descriptions)
}

func TestMonitorDropsInvalidEvents(t *testing.T) {
	for name, e := range map[string]Event{
		"unknown kind":                 {EventID: spotID, Kind: "STATE_CHANGE"},
		"empty event ID":               {Kind: monitor.SpotITNKind},
		"event ID of another kind":     {EventID: "rebalance-recommendation-" + strings.Repeat("ab", 32), Kind: monitor.SpotITNKind},
		"event ID with text":           {EventID: spotID + " <!channel>", Kind: monitor.SpotITNKind},
		"scheduled event without code": {EventID: "instance-event-0d59937288b749b32", Kind: monitor.ScheduledEventKind},
		"unknown scheduled event code": {EventID: "instance-event-0d59937288b749b32", Kind: monitor.ScheduledEventKind, Code: "<!channel>"},
		"unknown scheduled event state": {
			EventID: "instance-event-0d59937288b749b32", Kind: monitor.ScheduledEventKind, Code: "system-reboot", State: "\"}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			mh := newMonitorHarness()
			mh.setReport(t, newFakeNode(testNode), Report{Events: []Event{e, {EventID: spotID, Kind: monitor.SpotITNKind}}})
			h.Ok(t, mh.monitor.Monitor())
			// the valid event of the report still counts
			events := drain(mh.interruption)
			h.Equals(t, 1, len(events))
			h.Equals(t, monitor.SpotITNKind, events[0].Kind)
		})
	}
}

func TestMonitorKeepsUncordonAfterRebootToRestartingScheduledEvents(t *testing.T) {
	mh := newMonitorHarness()
	mh.setReport(t, newFakeNode(testNode), Report{Events: []Event{
		{EventID: "instance-event-1", Kind: monitor.ScheduledEventKind, Code: "system-maintenance", UncordonAfterReboot: true},
		{EventID: "instance-event-2", Kind: monitor.ScheduledEventKind, Code: "instance-reboot", UncordonAfterReboot: true},
		{EventID: spotID, Kind: monitor.SpotITNKind, UncordonAfterReboot: true, State: "active"},
	}})
	h.Ok(t, mh.monitor.Monitor())
	tasks := map[string]bool{}
	for _, e := range drain(mh.interruption) {
		tasks[e.EventID] = e.Kind == monitor.ScheduledEventKind && e.PreDrainTask != nil
		if e.Kind == monitor.SpotITNKind {
			h.Equals(t, "", e.State)
		}
	}
	h.Equals(t, false, tasks["instance-event-1"])
	h.Equals(t, true, tasks["instance-event-2"])
}

func TestNodeMetadataDropsInvalidReportedMetadata(t *testing.T) {
	n := newFakeNode(testNode)
	h.Equals(t, "123456789012", NodeMetadata(n, ReportedMetadata{AccountID: "123456789012"}).AccountId)
	h.Equals(t, "", NodeMetadata(n, ReportedMetadata{AccountID: "1234567890123"}).AccountId)
	h.Equals(t, "", NodeMetadata(n, ReportedMetadata{AccountID: `1","text":"<!channel>`}).AccountId)
	h.Equals(t, "on-demand", NodeMetadata(n, ReportedMetadata{InstanceLifeCycle: "on-demand"}).InstanceLifeCycle)
	h.Equals(t, "", NodeMetadata(n, ReportedMetadata{InstanceLifeCycle: "spot\n<!channel>"}).InstanceLifeCycle)
}
