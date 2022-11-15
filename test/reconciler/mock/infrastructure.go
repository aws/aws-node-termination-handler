/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kubectl "k8s.io/kubectl/pkg/drain"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/event"
	asgterminateeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/asgterminate/v1"
	asgterminateeventv2 "github.com/aws/aws-node-termination-handler/pkg/event/asgterminate/v2"
	rebalancerecommendationeventv0 "github.com/aws/aws-node-termination-handler/pkg/event/rebalancerecommendation/v0"
	scheduledchangeeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/scheduledchange/v1"
	spotinterruptioneventv1 "github.com/aws/aws-node-termination-handler/pkg/event/spotinterruption/v1"
	statechangeeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/statechange/v1"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	kubectlcordondrain "github.com/aws/aws-node-termination-handler/pkg/node/cordondrain/kubectl"
	nodename "github.com/aws/aws-node-termination-handler/pkg/node/name"
	"github.com/aws/aws-node-termination-handler/pkg/sqsmessage"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
	terminatoradapter "github.com/aws/aws-node-termination-handler/pkg/terminator/adapter"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"

	"github.com/aws/aws-sdk-go/aws"
	awsrequest "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type (
	Infrastructure struct {
		// Input variables
		// These variables are assigned default values during test setup but may be
		// modified to represent different cluster states or AWS service responses.

		// Terminators currently in the cluster.
		Terminators map[types.NamespacedName]*v1alpha1.Terminator
		// Nodes currently in the cluster.
		Nodes map[types.NamespacedName]*v1.Node
		// Maps an EC2 instance id to an ASG lifecycle action state value.
		ASGLifecycleActions map[EC2InstanceID]State
		// Maps an EC2 instance id to the corresponding reservation for a node
		// in the cluster.
		EC2Reservations map[EC2InstanceID]*ec2.Reservation
		// Maps a queue URL to a list of messages waiting to be fetched.
		SQSQueues map[SQSQueueURL][]*sqs.Message

		// Output variables
		// These variables may be modified during reconciliation and should be
		// used to verify the resulting cluster state.

		// A lookup table for nodes that were cordoned.
		CordonedNodes map[NodeName]bool
		// A lookup table for nodes that were drained.
		DrainedNodes map[NodeName]bool

		// Other variables

		// Names of all nodes currently in cluster.
		NodeNames []NodeName
		// Instance IDs for all nodes currently in cluster.
		InstanceIDs []EC2InstanceID
		// Requests sent to the configured webhook.
		WebhookRequests []*http.Request

		// Name of default terminator.
		TerminatorNamespaceName types.NamespacedName
		// Default inputs to .Reconciler.Reconcile()
		Ctx     context.Context
		Request reconcile.Request

		// The reconciler instance under test.
		Reconciler terminator.Reconciler

		// Stubs
		// Default implementations interract with the backing variables listed
		// above. A test may put in place alternate behavior when needed.
		CompleteASGLifecycleActionFunc CompleteASGLifecycleActionFunc
		DescribeEC2InstancesFunc       DescribeEC2InstancesFunc
		KubeGetFunc                    KubeGetFunc
		ReceiveSQSMessageFunc          ReceiveSQSMessageFunc
		DeleteSQSMessageFunc           DeleteSQSMessageFunc
		CordonFunc                     kubectlcordondrain.CordonFunc
		DrainFunc                      kubectlcordondrain.DrainFunc
		WebhookSendFunc                webhook.HttpSendFunc
	}

	EC2InstanceID = string
	SQSQueueURL   = string
	NodeName      = string

	State string
)

const (
	QueueURL = "http://fake-queue.sqs.aws"

	StatePending  = State("pending")
	StateComplete = State("complete")
)

// NewInfrastructure creates a new mock set of AWS and Kubernetes resources.
//
// Starting state:
// * One terminator configured to reach from a mock SQS queue
// * Zero nodes
//
// Tests should modify the resources as needed.
func NewInfrastructure() *Infrastructure {
	infra := &Infrastructure{}

	// 1. Initialize variables.

	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.AddSync(io.Discard),
		zap.NewAtomicLevelAt(zap.DebugLevel),
	))

	infra.Ctx = logging.WithLogger(context.Background(), logger.Sugar())
	infra.TerminatorNamespaceName = types.NamespacedName{Namespace: "test", Name: "foo"}
	infra.Request = reconcile.Request{NamespacedName: infra.TerminatorNamespaceName}

	infra.SQSQueues = map[SQSQueueURL][]*sqs.Message{QueueURL: {}}
	infra.Terminators = map[types.NamespacedName]*v1alpha1.Terminator{
		// For convenience create a terminator that points to the sqs queue.
		infra.TerminatorNamespaceName: {
			Spec: v1alpha1.TerminatorSpec{
				SQS: v1alpha1.SQSSpec{
					QueueURL: QueueURL,
				},
			},
		},
	}
	infra.Nodes = map[types.NamespacedName]*v1.Node{}
	infra.EC2Reservations = map[EC2InstanceID]*ec2.Reservation{}
	infra.CordonedNodes = map[NodeName]bool{}
	infra.DrainedNodes = map[NodeName]bool{}

	infra.NodeNames = []NodeName{}
	infra.InstanceIDs = []EC2InstanceID{}
	infra.ASGLifecycleActions = map[EC2InstanceID]State{}

	infra.WebhookRequests = []*http.Request{}
	infra.WebhookSendFunc = func(req *http.Request) (*http.Response, error) {
		infra.WebhookRequests = append(infra.WebhookRequests, req)
		return &http.Response{StatusCode: 200}, nil
	}

	// 2. Setup stub clients.

	infra.DescribeEC2InstancesFunc = func(ctx aws.Context, input *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		output := ec2.DescribeInstancesOutput{}
		for _, instanceID := range input.InstanceIds {
			if instanceID == nil {
				continue
			}
			if reservation, found := infra.EC2Reservations[*instanceID]; found {
				output.Reservations = append(output.Reservations, reservation)
			}
		}
		return &output, nil
	}

	ec2Client := EC2Client(func(ctx aws.Context, input *ec2.DescribeInstancesInput, options ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
		return infra.DescribeEC2InstancesFunc(ctx, input, options...)
	})

	infra.CompleteASGLifecycleActionFunc = func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		Expect(input.InstanceId).ToNot(BeNil())
		if state, found := infra.ASGLifecycleActions[*input.InstanceId]; found {
			Expect(state).ToNot(Equal(StateComplete))
			infra.ASGLifecycleActions[*input.InstanceId] = StateComplete
		}
		return &autoscaling.CompleteLifecycleActionOutput{}, nil
	}

	asgClient := ASGClient(func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, options ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
		return infra.CompleteASGLifecycleActionFunc(ctx, input, options...)
	})

	infra.ReceiveSQSMessageFunc = func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		Expect(input.QueueUrl).ToNot(BeNil())

		messages, found := infra.SQSQueues[*input.QueueUrl]
		Expect(found).To(BeTrue(), "SQS queue does not exist: %q", *input.QueueUrl)

		return &sqs.ReceiveMessageOutput{Messages: append([]*sqs.Message{}, messages...)}, nil
	}

	infra.DeleteSQSMessageFunc = func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		Expect(input.QueueUrl).ToNot(BeNil())

		queue, found := infra.SQSQueues[*input.QueueUrl]
		Expect(found).To(BeTrue(), "SQS queue does not exist: %q", *input.QueueUrl)

		updatedQueue := make([]*sqs.Message, 0, len(queue))
		for i, m := range queue {
			if m.ReceiptHandle == input.ReceiptHandle {
				updatedQueue = append(updatedQueue, queue[:i]...)
				updatedQueue = append(updatedQueue, queue[i+1:]...)
				break
			}
		}
		infra.SQSQueues[*input.QueueUrl] = updatedQueue

		return &sqs.DeleteMessageOutput{}, nil
	}

	sqsClient := SQSClient{
		ReceiveSQSMessageFunc: func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
			return infra.ReceiveSQSMessageFunc(ctx, input, options...)
		},
		DeleteSQSMessageFunc: func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
			return infra.DeleteSQSMessageFunc(ctx, input, options...)
		},
	}

	infra.KubeGetFunc = func(ctx context.Context, key client.ObjectKey, object client.Object) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		switch out := object.(type) {
		case *v1.Node:
			n, found := infra.Nodes[key]
			if !found {
				return k8serrors.NewNotFound(schema.GroupResource{}, key.String())
			}
			*out = *n

		case *v1alpha1.Terminator:
			t, found := infra.Terminators[key]
			if !found {
				return k8serrors.NewNotFound(schema.GroupResource{}, key.String())
			}
			*out = *t

		default:
			return fmt.Errorf("unknown type: %s", reflect.TypeOf(object).Name())
		}
		return nil
	}

	kubeClient := KubeClient(func(ctx context.Context, key client.ObjectKey, object client.Object) error {
		return infra.KubeGetFunc(ctx, key, object)
	})

	infra.CordonFunc = func(_ *kubectl.Helper, node *v1.Node, desired bool) error {
		if _, found := infra.Nodes[types.NamespacedName{Name: node.Name}]; !found {
			return fmt.Errorf("node does not exist: %q", node.Name)
		}
		infra.CordonedNodes[node.Name] = true
		return nil
	}

	infra.DrainFunc = func(_ *kubectl.Helper, nodeName string) error {
		if _, found := infra.Nodes[types.NamespacedName{Name: nodeName}]; !found {
			return fmt.Errorf("node does not exist: %q", nodeName)
		}
		infra.DrainedNodes[nodeName] = true
		return nil
	}

	// 3. Construct the reconciler.

	eventParser := event.NewAggregatedParser(
		asgterminateeventv1.Parser{ASGLifecycleActionCompleter: asgClient},
		asgterminateeventv2.Parser{ASGLifecycleActionCompleter: asgClient},
		rebalancerecommendationeventv0.Parser{},
		scheduledchangeeventv1.Parser{},
		spotinterruptioneventv1.Parser{},
		statechangeeventv1.Parser{},
	)

	cordoner := kubectlcordondrain.CordonFunc(func(h *kubectl.Helper, n *v1.Node, d bool) error {
		return infra.CordonFunc(h, n, d)
	})

	drainer := kubectlcordondrain.DrainFunc(func(h *kubectl.Helper, n string) error {
		return infra.DrainFunc(h, n)
	})

	cordonDrainerBuilder := kubectlcordondrain.Builder{
		ClientSet: &kubernetes.Clientset{},
		Cordoner:  cordoner,
		Drainer:   drainer,
	}

	newHttpClientDoFunc := func(_ webhook.ProxyFunc) webhook.HttpSendFunc {
		return infra.WebhookSendFunc
	}

	infra.Reconciler = terminator.Reconciler{
		Name:            "terminator",
		RequeueInterval: time.Duration(10) * time.Second,
		NodeGetterBuilder: terminatoradapter.NodeGetterBuilder{
			NodeGetter: node.Getter{KubeGetter: kubeClient},
		},
		NodeNameGetter: nodename.Getter{EC2InstancesDescriber: ec2Client},
		SQSClientBuilder: terminatoradapter.SQSMessageClientBuilder{
			SQSMessageClient: sqsmessage.Client{SQSClient: sqsClient},
		},
		SQSMessageParser: terminatoradapter.EventParser{Parser: eventParser},
		Getter:           terminatoradapter.Getter{KubeGetter: kubeClient},
		CordonDrainerBuilder: terminatoradapter.CordonDrainerBuilder{
			Builder: cordonDrainerBuilder,
		},
		WebhookClientBuilder: terminatoradapter.WebhookClientBuilder(
			webhook.ClientBuilder(newHttpClientDoFunc).NewClient,
		),
	}

	return infra
}

// Change count of nodes in cluster.
func (m *Infrastructure) ResizeCluster(newNodeCount uint) {
	for currNodeCount := uint(len(m.Nodes)); currNodeCount < newNodeCount; currNodeCount++ {
		nodeName := fmt.Sprintf("node-%d", currNodeCount)
		m.NodeNames = append(m.NodeNames, nodeName)
		m.Nodes[types.NamespacedName{Name: nodeName}] = &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}

		instanceID := fmt.Sprintf("instance-%d", currNodeCount)
		m.InstanceIDs = append(m.InstanceIDs, instanceID)
		m.EC2Reservations[instanceID] = &ec2.Reservation{
			Instances: []*ec2.Instance{
				{PrivateDnsName: aws.String(nodeName)},
			},
		}
	}

	m.NodeNames = m.NodeNames[:newNodeCount]
	m.InstanceIDs = m.InstanceIDs[:newNodeCount]
}

// Create an ASG lifecycle action state entry for an EC2 instance ID.
func (m *Infrastructure) CreatePendingASGLifecycleAction(instanceID EC2InstanceID) {
	Expect(m.ASGLifecycleActions).ToNot(HaveKey(instanceID))
	m.ASGLifecycleActions[instanceID] = StatePending
}

// Reconcile runs a reconciliation with the default context and request, and returns the
// result and any error that occured.
func (m *Infrastructure) Reconcile() (reconcile.Result, error) {
	return m.Reconciler.Reconcile(m.Ctx, m.Request)
}
