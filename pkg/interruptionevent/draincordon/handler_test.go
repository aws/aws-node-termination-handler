package draincordon

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubectl/pkg/drain"
)

func TestHandleEvent_PreDrainFailureSkipsCordonAndPostDrainTask(t *testing.T) {
	const nodeName = "test-node"
	preDrainErr := errors.New("taint update failed")
	postDrainCalls := 0

	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	})
	testNode, err := node.NewWithValues(config.Config{}, &drain.Helper{
		Ctx:    context.TODO(),
		Client: client,
	}, uptime.Uptime)
	if err != nil {
		t.Fatalf("create test node: %v", err)
	}

	nthConfig := config.Config{CordonOnly: true}
	handler := New(
		interruptioneventstore.New(nthConfig),
		*testNode,
		nthConfig,
		ec2metadata.NodeMetadata{},
		observability.Metrics{},
		observability.K8sEventRecorder{},
	)
	drainEvent := &monitor.InterruptionEvent{
		EventID:  "taint-failure-event",
		Kind:     monitor.RebalanceRecommendationKind,
		NodeName: nodeName,
		PreDrainTask: func(monitor.InterruptionEvent, node.Node) error {
			return preDrainErr
		},
		PostDrainTask: func(monitor.InterruptionEvent, node.Node) error {
			postDrainCalls++
			return nil
		},
	}

	err = handler.HandleEvent(drainEvent)
	if !errors.Is(err, preDrainErr) {
		t.Fatalf("expected pre-drain error %v, got %v", preDrainErr, err)
	}
	if postDrainCalls != 0 {
		t.Fatalf("post-drain task was called %d times after pre-drain failure", postDrainCalls)
	}

	updatedNode, err := client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get test node: %v", err)
	}
	if updatedNode.Spec.Unschedulable {
		t.Fatal("node was cordoned after pre-drain failure")
	}
}
