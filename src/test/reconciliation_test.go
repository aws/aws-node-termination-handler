/*
Copyright 2022.

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

package test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/zap"

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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	awsrequest "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type (
	Ec2InstanceId = string
	SqsQueueUrl   = string
	NodeName      = string

	State string
)

const (
	StatePending  = State("pending")
	StateComplete = State("complete")
)

var _ = Describe("Reconciliation", func() {
	const (
		errMsg   = "test error"
		queueUrl = "http://fake-queue.sqs.aws"
	)

	var (
		// Input variables
		// These variables are assigned default values during test setup but may be
		// modified to represent different cluster states or AWS service responses.

		// Terminators currently in the cluster.
		terminators map[types.NamespacedName]*v1alpha1.Terminator
		// Nodes currently in the cluster.
		nodes map[types.NamespacedName]*v1.Node
		// Maps an EC2 instance id to an ASG lifecycle action state value.
		asgLifecycleActions map[Ec2InstanceId]State
		// Maps an EC2 instance id to the corresponding reservation for a node
		// in the cluster.
		ec2Reservations map[Ec2InstanceId]*ec2.Reservation
		// Maps a queue URL to a list of messages waiting to be fetched.
		sqsQueues map[SqsQueueUrl][]*sqs.Message

		// Output variables
		// These variables may be modified during reconciliation and should be
		// used to verify the resulting cluster state.

		// A lookup table for nodes that were cordoned.
		cordonedNodes map[NodeName]bool
		// A lookup table for nodes that were drained.
		drainedNodes map[NodeName]bool
		// Result information returned by the reconciler.
		result reconcile.Result
		// Error information returned by the reconciler.
		err error

		// Other variables

		// Names of all nodes currently in cluster.
		nodeNames []NodeName
		// Instance IDs for all nodes currently in cluster.
		instanceIds []Ec2InstanceId
		// Change count of nodes in cluster.
		resizeCluster func(nodeCount uint)
		// Create an ASG lifecycle action state entry for an EC2 instance ID.
		createPendingAsgLifecycleAction func(Ec2InstanceId)

		// Name of default terminator.
		terminatorNamespaceName types.NamespacedName
		// Inputs to .Reconcile()
		ctx     context.Context
		request reconcile.Request

		// The reconciler instance under test.
		reconciler terminator.Reconciler

		// Stubs
		// Default implementations interract with the backing variables listed
		// above. A test may put in place alternate behavior when needed.
		completeAsgLifecycleActionFunc CompleteAsgLifecycleActionFunc
		describeEc2InstancesFunc       DescribeEc2InstancesFunc
		kubeGetFunc                    KubeGetFunc
		receiveSqsMessageFunc          ReceiveSqsMessageFunc
		deleteSqsMessageFunc           DeleteSqsMessageFunc
		cordonFunc                     kubectlcordondrain.RunCordonFunc
		drainFunc                      kubectlcordondrain.RunDrainFunc
	)

	When("the SQS queue is empty", func() {
		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("the SQS queue contains an ASG Lifecycle Notification v1", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			createPendingAsgLifecycleAction(instanceIds[1])
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("completes the ASG lifecycle action", func() {
			Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIds[1], Equal(StateComplete)), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains an ASG Lifecycle Notification v2", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "2",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			createPendingAsgLifecycleAction(instanceIds[1])
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("completes the ASG lifecycle action", func() {
			Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIds[1], Equal(StateComplete)), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a Rebalance Recommendation Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance Rebalance Recommendation",
					"version": "0",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a Scheduled Change Notification", func() {
		BeforeEach(func() {
			resizeCluster(4)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.health",
					"detail-type": "AWS Health Event",
					"version": "1",
					"detail": {
						"service": "EC2",
						"eventTypeCategory": "scheduledChange",
						"affectedEntities": [
							{"entityValue": "%s"},
							{"entityValue": "%s"}
						]
					}
				}`, instanceIds[1], instanceIds[2])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted nodes", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveKey(nodeNames[2]), HaveLen(2)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveKey(nodeNames[2]), HaveLen(2)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a Spot Interruption Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a State Change (stopping) Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance State-change Notification",
					"version": "1",
					"detail": {
						"instance-id": "%s",
						"state": "stopping"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted nodes", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a State Change (stopped) Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance State-change Notification",
					"version": "1",
					"detail": {
						"instance-id": "%s",
						"state": "stopped"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a State Change (shutting-down) Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance State-change Notification",
					"version": "1",
					"detail": {
						"instance-id": "%s",
						"state": "shutting-down"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a State Change (terminated) Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance State-change Notification",
					"version": "1",
					"detail": {
						"instance-id": "%s",
						"state": "terminated"
					}
				}`, instanceIds[1])),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains multiple messages", func() {
		BeforeEach(func() {
			resizeCluster(12)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl],
				&sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIds[1])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-2"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIds[2])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-3"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance Rebalance Recommendation",
						"version": "0",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIds[3])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-4"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.health",
						"detail-type": "AWS Health Event",
						"version": "1",
						"detail": {
							"service": "EC2",
							"eventTypeCategory": "scheduledChange",
							"affectedEntities": [
								{"entityValue": "%s"},
								{"entityValue": "%s"}
							]
						}
					}`, instanceIds[4], instanceIds[5])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-5"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIds[6])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-6"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, instanceIds[7])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-7"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopped"
						}
					}`, instanceIds[8])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-8"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "shutting-down"
						}
					}`, instanceIds[9])),
				},
				&sqs.Message{
					ReceiptHandle: aws.String("msg-9"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "terminated"
						}
					}`, instanceIds[10])),
				},
			)
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted nodes", func() {
			Expect(cordonedNodes).To(And(
				HaveKey(nodeNames[1]),
				HaveKey(nodeNames[2]),
				HaveKey(nodeNames[3]),
				HaveKey(nodeNames[4]),
				HaveKey(nodeNames[5]),
				HaveKey(nodeNames[6]),
				HaveKey(nodeNames[7]),
				HaveKey(nodeNames[8]),
				HaveKey(nodeNames[9]),
				HaveKey(nodeNames[10]),
				HaveLen(10),
			))
			Expect(drainedNodes).To(And(
				HaveKey(nodeNames[1]),
				HaveKey(nodeNames[2]),
				HaveKey(nodeNames[3]),
				HaveKey(nodeNames[4]),
				HaveKey(nodeNames[5]),
				HaveKey(nodeNames[6]),
				HaveKey(nodeNames[7]),
				HaveKey(nodeNames[8]),
				HaveKey(nodeNames[9]),
				HaveKey(nodeNames[10]),
				HaveLen(10),
			))
		})

		It("deletes the messages from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS queue contains an unrecognized message", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(`{
					"source": "test.suite",
					"detail-type": "Not a real notification",
					"version": "1",
					"detail": {}
				}`),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the SQS message cannot be parsed", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(`{
					"source": "test.suite",
					"detail-type": "Mal-formed notification",
				`),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the terminator cannot be found", func() {
		BeforeEach(func() {
			delete(terminators, terminatorNamespaceName)
		})

		It("returns success but does not requeue the request", func() {
			Expect(result, err).To(BeZero())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("there is an error getting the terminator", func() {
		BeforeEach(func() {
			defaultKubeGetFunc := kubeGetFunc
			kubeGetFunc = func(ctx context.Context, key client.ObjectKey, object client.Object) error {
				switch object.(type) {
				case *v1alpha1.Terminator:
					return errors.New(errMsg)
				default:
					return defaultKubeGetFunc(ctx, key, object)
				}
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})
	})

	When("there is an error getting SQS messages", func() {
		BeforeEach(func() {
			receiveSqsMessageFunc = func(_ aws.Context, _ *sqs.ReceiveMessageInput, _ ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
				return nil, errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})
	})

	When("there is an error getting the EC2 reservation for the instance ID", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			describeEc2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
				return nil, errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("there is no EC2 reservation for the instance ID", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			describeEc2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{},
				}, nil
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("the EC2 reservation contains no instances", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			describeEc2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{
						{Instances: []*ec2.Instance{}},
					},
				}, nil
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("the EC2 reservation's instance has no PrivateDnsName", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			describeEc2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{
						{
							Instances: []*ec2.Instance{
								{PrivateDnsName: nil},
							},
						},
					},
				}, nil
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("the EC2 reservation's instance's PrivateDnsName empty", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			describeEc2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{
						{
							Instances: []*ec2.Instance{
								{PrivateDnsName: aws.String("")},
							},
						},
					},
				}, nil
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("there is an error getting the cluster node name for an EC2 instance ID", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			defaultKubeGetFunc := kubeGetFunc
			kubeGetFunc = func(ctx context.Context, key client.ObjectKey, object client.Object) error {
				switch object.(type) {
				case *v1.Node:
					return errors.New(errMsg)
				default:
					return defaultKubeGetFunc(ctx, key, object)
				}
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("cordoning a node fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			cordonFunc = func(_ *kubectl.Helper, _ *v1.Node, _ bool) error {
				return errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("draining a node fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})

			drainFunc = func(_ *kubectl.Helper, _ string) error {
				return errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("cordons the target node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("does not drain the target node", func() {
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("completing an ASG Lifecycle Action (v1) fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			completeAsgLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				return nil, errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the request to complete the ASG Lifecycle Action (v1) fails with a status != 400", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			completeAsgLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				return nil, awserr.NewRequestFailure(awserr.New("", errMsg, errors.New(errMsg)), 404, "")
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(HaveLen(1))
		})
	})

	When("completing an ASG Lifecycle Action (v2) fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "2",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			completeAsgLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				return nil, errors.New(errMsg)
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(BeEmpty())
		})
	})

	When("the request to complete the ASG Lifecycle Action (v2) fails with a status != 400", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "2",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIds[1])),
			})

			completeAsgLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				return nil, awserr.NewRequestFailure(awserr.New("", errMsg, errors.New(errMsg)), 404, "")
			}
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(MatchError(ContainSubstring(errMsg)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
		})

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueUrl]).To(HaveLen(1))
		})
	})

	When("getting messages from a terminator's SQS queue", func() {
		const (
			maxNumberOfMessages      = int64(4)
			visibilityTimeoutSeconds = int64(17)
			waitTimeSeconds          = int64(31)
		)
		var (
			attributeNames        = []string{"TestAttributeName1", "TestAttributeName2"}
			messageAttributeNames = []string{"TestMsgAttributeName1", "TestMsgAttributeName2"}
			input                 *sqs.ReceiveMessageInput
		)

		BeforeEach(func() {
			terminator, found := terminators[terminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.Sqs.MaxNumberOfMessages = maxNumberOfMessages
			terminator.Spec.Sqs.QueueUrl = queueUrl
			terminator.Spec.Sqs.VisibilityTimeoutSeconds = visibilityTimeoutSeconds
			terminator.Spec.Sqs.WaitTimeSeconds = waitTimeSeconds
			terminator.Spec.Sqs.AttributeNames = append([]string{}, attributeNames...)
			terminator.Spec.Sqs.MessageAttributeNames = append([]string{}, messageAttributeNames...)

			defaultReceiveSqsMessageFunc := receiveSqsMessageFunc
			receiveSqsMessageFunc = func(ctx aws.Context, in *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
				input = in
				return defaultReceiveSqsMessageFunc(ctx, in, options...)
			}
		})

		It("sends the input values from the terminator", func() {
			Expect(input).ToNot(BeNil())

			for i, attrName := range input.AttributeNames {
				Expect(attrName).ToNot(BeNil())
				Expect(*attrName).To(Equal(attributeNames[i]))
			}
			for i, attrName := range input.MessageAttributeNames {
				Expect(attrName).ToNot(BeNil())
				Expect(*attrName).To(Equal(messageAttributeNames[i]))
			}

			Expect(input.MaxNumberOfMessages).ToNot(BeNil())
			Expect(*input.MaxNumberOfMessages).To(Equal(maxNumberOfMessages))

			Expect(input.QueueUrl).ToNot(BeNil())
			Expect(*input.QueueUrl).To(Equal(queueUrl))

			Expect(input.VisibilityTimeout).ToNot(BeNil())
			Expect(*input.VisibilityTimeout).To(Equal(visibilityTimeoutSeconds))

			Expect(input.WaitTimeSeconds).ToNot(BeNil())
			Expect(*input.WaitTimeSeconds).To(Equal(waitTimeSeconds))
		})
	})

	When("cordoning a node", func() {
		const (
			force               = true
			gracePeriodSeconds  = 31
			ignoreAllDaemonSets = true
			deleteEmptyDirData  = true
		)
		var helper *kubectl.Helper
		var timeout time.Duration

		BeforeEach(func() {
			timeout = 42 * time.Second

			terminator, found := terminators[terminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.Drain.DeleteEmptyDirData = deleteEmptyDirData
			terminator.Spec.Drain.Force = force
			terminator.Spec.Drain.GracePeriodSeconds = gracePeriodSeconds
			terminator.Spec.Drain.IgnoreAllDaemonSets = ignoreAllDaemonSets
			terminator.Spec.Drain.TimeoutSeconds = int(timeout.Seconds())

			defaultCordonFunc := cordonFunc
			cordonFunc = func(h *kubectl.Helper, node *v1.Node, desired bool) error {
				helper = h
				return defaultCordonFunc(h, node, desired)
			}

			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})
		})

		It("sends the input values from the terminator", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(And(
				HaveField("DeleteEmptyDirData", Equal(deleteEmptyDirData)),
				HaveField("Force", Equal(force)),
				HaveField("GracePeriodSeconds", Equal(gracePeriodSeconds)),
				HaveField("IgnoreAllDaemonSets", Equal(ignoreAllDaemonSets)),
				HaveField("Timeout", Equal(timeout)),
			))
		})

		It("sends additional input values", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(And(
				HaveField("Client", Not(BeNil())),
				HaveField("Ctx", Not(BeNil())),
				HaveField("Out", Not(BeNil())),
				HaveField("ErrOut", Not(BeNil())),
			))
		})
	})

	When("draining a node", func() {
		const (
			force               = true
			gracePeriodSeconds  = 31
			ignoreAllDaemonSets = true
			deleteEmptyDirData  = true
		)
		var helper *kubectl.Helper
		var timeout time.Duration

		BeforeEach(func() {
			timeout = 42 * time.Second

			terminator, found := terminators[terminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.Drain.DeleteEmptyDirData = deleteEmptyDirData
			terminator.Spec.Drain.Force = force
			terminator.Spec.Drain.GracePeriodSeconds = gracePeriodSeconds
			terminator.Spec.Drain.IgnoreAllDaemonSets = ignoreAllDaemonSets
			terminator.Spec.Drain.TimeoutSeconds = int(timeout.Seconds())

			defaultDrainFunc := drainFunc
			drainFunc = func(h *kubectl.Helper, nodeName string) error {
				helper = h
				return defaultDrainFunc(h, nodeName)
			}

			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIds[1])),
			})
		})

		It("sends the input values from the terminator", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(And(
				HaveField("DeleteEmptyDirData", Equal(deleteEmptyDirData)),
				HaveField("Force", Equal(force)),
				HaveField("GracePeriodSeconds", Equal(gracePeriodSeconds)),
				HaveField("IgnoreAllDaemonSets", Equal(ignoreAllDaemonSets)),
				HaveField("Timeout", Equal(timeout)),
			))
		})

		It("sends additional values", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(And(
				HaveField("Client", Not(BeNil())),
				HaveField("Ctx", Not(BeNil())),
				HaveField("Out", Not(BeNil())),
				HaveField("ErrOut", Not(BeNil())),
			))
		})
	})

	When("completing an ASG Complete Lifecycle Action", func() {
		const (
			autoScalingGroupName  = "testAutoScalingGroupName"
			lifecycleActionResult = "CONTINUE"
			lifecycleHookName     = "testLifecycleHookName"
			lifecycleActionToken  = "testLifecycleActionToken"
		)
		var input *autoscaling.CompleteLifecycleActionInput

		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueUrl] = append(sqsQueues[queueUrl], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"AutoScalingGroupName": "%s",
						"EC2InstanceId": "%s",
						"LifecycleActionToken": "%s",
						"LifecycleHookName": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, autoScalingGroupName, instanceIds[1], lifecycleActionToken, lifecycleHookName)),
			})

			defaultCompleteAsgLifecycleActionFunc := completeAsgLifecycleActionFunc
			completeAsgLifecycleActionFunc = func(ctx aws.Context, in *autoscaling.CompleteLifecycleActionInput, options ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				input = in
				return defaultCompleteAsgLifecycleActionFunc(ctx, in, options...)
			}
		})

		It("sends the expected input values", func() {
			Expect(input).ToNot(BeNil())

			Expect(input.AutoScalingGroupName).ToNot(BeNil())
			Expect(*input.AutoScalingGroupName).To(Equal(autoScalingGroupName))

			Expect(input.LifecycleActionResult).ToNot(BeNil())
			Expect(*input.LifecycleActionResult).To(Equal(lifecycleActionResult))

			Expect(input.LifecycleHookName).ToNot(BeNil())
			Expect(*input.LifecycleHookName).To(Equal(lifecycleHookName))

			Expect(input.LifecycleActionToken).ToNot(BeNil())
			Expect(*input.LifecycleActionToken).To(Equal(lifecycleActionToken))

			Expect(input.InstanceId).ToNot(BeNil())
			Expect(*input.InstanceId).To(Equal(instanceIds[1]))
		})
	})

	// Setup the starter state:
	// * One terminator (terminatorNamedspacedName)
	// * The terminator references an empty sqs queue (queueUrl)
	// * Zero nodes (use resizeCluster())
	//
	// Tests should modify the cluster/aws service states as needed.
	BeforeEach(func() {
		// 1. Initialize variables.

		ctx = logging.WithLogger(context.Background(), zap.NewNop().Sugar())
		terminatorNamespaceName = types.NamespacedName{Namespace: "test", Name: "foo"}
		request = reconcile.Request{NamespacedName: terminatorNamespaceName}
		sqsQueues = map[SqsQueueUrl][]*sqs.Message{queueUrl: {}}
		terminators = map[types.NamespacedName]*v1alpha1.Terminator{
			// For convenience create a terminator that points to the sqs queue.
			terminatorNamespaceName: {
				Spec: v1alpha1.TerminatorSpec{
					Sqs: v1alpha1.SqsSpec{
						QueueUrl: queueUrl,
					},
				},
			},
		}
		nodes = map[types.NamespacedName]*v1.Node{}
		ec2Reservations = map[Ec2InstanceId]*ec2.Reservation{}
		cordonedNodes = map[NodeName]bool{}
		drainedNodes = map[NodeName]bool{}

		nodeNames = []NodeName{}
		instanceIds = []Ec2InstanceId{}
		resizeCluster = func(newNodeCount uint) {
			for currNodeCount := uint(len(nodes)); currNodeCount < newNodeCount; currNodeCount++ {
				nodeName := fmt.Sprintf("node-%d", currNodeCount)
				nodeNames = append(nodeNames, nodeName)
				nodes[types.NamespacedName{Name: nodeName}] = &v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				}

				instanceId := fmt.Sprintf("instanceId-%d", currNodeCount)
				instanceIds = append(instanceIds, instanceId)
				ec2Reservations[instanceId] = &ec2.Reservation{
					Instances: []*ec2.Instance{
						{PrivateDnsName: aws.String(nodeName)},
					},
				}
			}

			nodeNames = nodeNames[:newNodeCount]
			instanceIds = instanceIds[:newNodeCount]
		}

		asgLifecycleActions = map[Ec2InstanceId]State{}
		createPendingAsgLifecycleAction = func(instanceId Ec2InstanceId) {
			Expect(asgLifecycleActions).ToNot(HaveKey(instanceId))
			asgLifecycleActions[instanceId] = StatePending
		}

		// 2. Setup stub clients.

		describeEc2InstancesFunc = func(ctx aws.Context, input *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			output := ec2.DescribeInstancesOutput{}
			for _, instanceId := range input.InstanceIds {
				if instanceId == nil {
					continue
				}
				if reservation, found := ec2Reservations[*instanceId]; found {
					output.Reservations = append(output.Reservations, reservation)
				}
			}
			return &output, nil
		}

		ec2Client := Ec2Client(func(ctx aws.Context, input *ec2.DescribeInstancesInput, options ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
			return describeEc2InstancesFunc(ctx, input, options...)
		})

		completeAsgLifecycleActionFunc = func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			Expect(input.InstanceId).ToNot(BeNil())
			if state, found := asgLifecycleActions[*input.InstanceId]; found {
				Expect(state).ToNot(Equal(StateComplete))
				asgLifecycleActions[*input.InstanceId] = StateComplete
			}
			return &autoscaling.CompleteLifecycleActionOutput{}, nil
		}

		asgClient := AsgClient(func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, options ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
			return completeAsgLifecycleActionFunc(ctx, input, options...)
		})

		receiveSqsMessageFunc = func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			Expect(input.QueueUrl).ToNot(BeNil())

			messages, found := sqsQueues[*input.QueueUrl]
			Expect(found).To(BeTrue(), "SQS queue does not exist: %q", *input.QueueUrl)

			return &sqs.ReceiveMessageOutput{Messages: append([]*sqs.Message{}, messages...)}, nil
		}

		deleteSqsMessageFunc = func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			Expect(input.QueueUrl).ToNot(BeNil())

			queue, found := sqsQueues[*input.QueueUrl]
			Expect(found).To(BeTrue(), "SQS queue does not exist: %q", *input.QueueUrl)

			updatedQueue := make([]*sqs.Message, 0, len(queue))
			for i, m := range queue {
				if m.ReceiptHandle == input.ReceiptHandle {
					updatedQueue = append(updatedQueue, queue[:i]...)
					updatedQueue = append(updatedQueue, queue[i+1:]...)
					break
				}
			}
			sqsQueues[*input.QueueUrl] = updatedQueue

			return &sqs.DeleteMessageOutput{}, nil
		}

		sqsClient := SqsClient{
			ReceiveSqsMessageFunc: func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
				return receiveSqsMessageFunc(ctx, input, options...)
			},
			DeleteSqsMessageFunc: func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
				return deleteSqsMessageFunc(ctx, input, options...)
			},
		}

		kubeGetFunc = func(ctx context.Context, key client.ObjectKey, object client.Object) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			switch out := object.(type) {
			case *v1.Node:
				n, found := nodes[key]
				if !found {
					return k8serrors.NewNotFound(schema.GroupResource{}, key.String())
				}
				*out = *n

			case *v1alpha1.Terminator:
				t, found := terminators[key]
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
			return kubeGetFunc(ctx, key, object)
		})

		cordonFunc = func(_ *kubectl.Helper, node *v1.Node, desired bool) error {
			if _, found := nodes[types.NamespacedName{Name: node.Name}]; !found {
				return fmt.Errorf("node does not exist: %q", node.Name)
			}
			cordonedNodes[node.Name] = true
			return nil
		}

		drainFunc = func(_ *kubectl.Helper, nodeName string) error {
			if _, found := nodes[types.NamespacedName{Name: nodeName}]; !found {
				return fmt.Errorf("node does not exist: %q", nodeName)
			}
			drainedNodes[nodeName] = true
			return nil
		}

		// 3. Construct the reconciler.

		nodeGetter, err := node.NewGetter(kubeClient)
		Expect(nodeGetter, err).ToNot(BeNil())

		nodeNameGetter, err := nodename.NewGetter(ec2Client)
		Expect(nodeNameGetter, err).ToNot(BeNil())

		asgTerminateEventV1Parser, err := asgterminateeventv1.NewParser(asgClient)
		Expect(asgTerminateEventV1Parser, err).ToNot(BeNil())

		asgTerminateEventV2Parser, err := asgterminateeventv2.NewParser(asgClient)
		Expect(asgTerminateEventV2Parser, err).ToNot(BeNil())

		sqsMessageParser, err := terminator.NewSqsMessageParser(event.NewParser(
			asgTerminateEventV1Parser,
			asgTerminateEventV2Parser,
			rebalancerecommendationeventv0.NewParser(),
			scheduledchangeeventv1.NewParser(),
			spotinterruptioneventv1.NewParser(),
			statechangeeventv1.NewParser(),
		))
		Expect(sqsMessageParser, err).ToNot(BeNil())

		terminatorGetter, err := terminator.NewGetter(kubeClient)
		Expect(terminatorGetter, err).ToNot(BeNil())

		sqsMessageClient, err := sqsmessage.NewClient(sqsClient)
		Expect(sqsMessageClient, err).ToNot(BeNil())

		terminatorSqsClientBuilder, err := terminator.NewSqsClientBuilder(sqsMessageClient)
		Expect(terminatorSqsClientBuilder, err).ToNot(BeNil())

		cordoner, err := kubectlcordondrain.NewCordoner(func(h *kubectl.Helper, n *v1.Node, d bool) error {
			return cordonFunc(h, n, d)
		})
		Expect(cordoner, err).ToNot(BeNil())

		drainer, err := kubectlcordondrain.NewDrainer(func(h *kubectl.Helper, n string) error {
			return drainFunc(h, n)
		})
		Expect(drainer, err).ToNot(BeNil())

		cordonDrainerBuilder, err := kubectlcordondrain.NewBuilder(&kubernetes.Clientset{}, cordoner, drainer)
		Expect(cordonDrainerBuilder, err).ToNot(BeNil())

		terminatorCordonDrainerBuilder, err := terminator.NewCordonDrainerBuilder(cordonDrainerBuilder)
		Expect(terminatorCordonDrainerBuilder, err).ToNot(BeNil())

		reconciler = terminator.Reconciler{
			Name:                 "terminator",
			RequeueInterval:      time.Duration(10) * time.Second,
			NodeGetter:           nodeGetter,
			NodeNameGetter:       nodeNameGetter,
			SqsClientBuilder:     terminatorSqsClientBuilder,
			SqsMessageParser:     sqsMessageParser,
			Getter:               terminatorGetter,
			CordonDrainerBuilder: terminatorCordonDrainerBuilder,
		}
	})

	// Run the reconciliation before each test subject.
	JustBeforeEach(func() {
		result, err = reconciler.Reconcile(ctx, request)
	})
})
