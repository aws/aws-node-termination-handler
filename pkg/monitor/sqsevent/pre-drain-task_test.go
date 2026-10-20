package sqsevent

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	"github.com/aws/aws-sdk-go/service/sqs"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/kubectl/pkg/drain"
)

const (
	taintFailureTestInstanceID = "i-0123456789"
	taintFailureTestNodeName   = "ip-10-0-0-157.us-east-2.compute.internal"
)

func TestPreDrainTaskReturnsTaintError(t *testing.T) {
	sqsMonitor := SQSMonitor{
		EC2: h.MockedEC2{
			DescribeInstancesResp: getDescribeInstancesResp(
				taintFailureTestInstanceID,
				taintFailureTestNodeName,
				map[string]string{ASGTagName: "test-asg"},
			),
		},
		ASG: &h.MockedASG{},
	}
	message := &sqs.Message{}

	tests := []struct {
		name  string
		build func() (*monitor.InterruptionEvent, error)
	}{
		{
			name: "ASG lifecycle termination",
			build: func() (*monitor.InterruptionEvent, error) {
				return sqsMonitor.asgTerminationToInterruptionEvent(&EventBridgeEvent{
					ID: "asg-event",
					Detail: []byte(`{
						"LifecycleActionToken":"token",
						"AutoScalingGroupName":"test-asg",
						"LifecycleHookName":"test-hook",
						"EC2InstanceId":"i-0123456789",
						"LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING"
					}`),
				}, message)
			},
		},
		{
			name: "rebalance recommendation",
			build: func() (*monitor.InterruptionEvent, error) {
				return sqsMonitor.rebalanceRecommendationToInterruptionEvent(&EventBridgeEvent{
					ID:     "rebalance-event",
					Detail: []byte(`{"instance-id":"i-0123456789"}`),
				}, message)
			},
		},
		{
			name: "scheduled change",
			build: func() (*monitor.InterruptionEvent, error) {
				wrappers := sqsMonitor.scheduledEventToInterruptionEvents(&EventBridgeEvent{
					ID: "scheduled-event",
					Detail: []byte(`{
						"eventTypeCategory":"scheduledChange",
						"service":"EC2",
						"affectedEntities":[{"entityValue":"i-0123456789"}]
					}`),
				}, message)
				if len(wrappers) != 1 {
					return nil, errors.New("expected one scheduled change interruption event")
				}
				return wrappers[0].InterruptionEvent, wrappers[0].Err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interruptionEvent, err := test.build()
			if err != nil {
				t.Fatalf("build interruption event: %v", err)
			}

			err = interruptionEvent.PreDrainTask(*interruptionEvent, failingTaintNode(t))
			if err == nil {
				t.Fatal("expected pre-drain task to return the taint update error")
			}
		})
	}
}

func failingTaintNode(t *testing.T) node.Node {
	t.Helper()

	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: taintFailureTestNodeName},
	})
	client.PrependReactor("update", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forced taint update failure")
	})

	testNode, err := node.NewWithValues(config.Config{
		TaintNode:         true,
		HeartbeatInterval: -1,
		HeartbeatUntil:    -1,
	}, &drain.Helper{
		Ctx:    context.TODO(),
		Client: client,
	}, uptime.Uptime)
	if err != nil {
		t.Fatalf("create test node: %v", err)
	}

	return *testNode
}
