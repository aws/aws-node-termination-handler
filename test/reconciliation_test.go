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

package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
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
	"github.com/aws/aws-sdk-go/aws/awserr"
	awsrequest "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type (
	EC2InstanceID = string
	SQSQueueURL   = string
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
		queueURL = "http://fake-queue.sqs.aws"
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
		asgLifecycleActions map[EC2InstanceID]State
		// Maps an EC2 instance id to the corresponding reservation for a node
		// in the cluster.
		ec2Reservations map[EC2InstanceID]*ec2.Reservation
		// Maps a queue URL to a list of messages waiting to be fetched.
		sqsQueues map[SQSQueueURL][]*sqs.Message

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
		instanceIDs []EC2InstanceID
		// Change count of nodes in cluster.
		resizeCluster func(nodeCount uint)
		// Create an ASG lifecycle action state entry for an EC2 instance ID.
		createPendingASGLifecycleAction func(EC2InstanceID)
		// Requests sent to the configured webhook.
		webhookRequests []*http.Request

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
		completeASGLifecycleActionFunc CompleteASGLifecycleActionFunc
		describeEC2InstancesFunc       DescribeEC2InstancesFunc
		kubeGetFunc                    KubeGetFunc
		receiveSQSMessageFunc          ReceiveSQSMessageFunc
		deleteSQSMessageFunc           DeleteSQSMessageFunc
		cordonFunc                     kubectlcordondrain.CordonFunc
		drainFunc                      kubectlcordondrain.DrainFunc
		webhookSendFunc                webhook.HttpSendFunc
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
		When("the lifecycle transition is termination", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons and drains only the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
				Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the lifecycle transition is not termination", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "test:INVALID"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain any nodes", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not complete the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StatePending)), HaveLen(1)))
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})
	})

	When("the SQS queue contains an ASG Lifecycle Notification v2", func() {
		When("the lifecycle transition is termination", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons and drains only the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
				Expect(drainedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the lifecycle transition is not termination", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "test:INVALID"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain any nodes", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not complete the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StatePending)), HaveLen(1)))
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})
	})

	When("the SQS queue contains a Rebalance Recommendation Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Instance Rebalance Recommendation",
					"version": "0",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a Scheduled Change Notification", func() {
		When("the service is EC2 and the event type category is scheduled change", func() {
			BeforeEach(func() {
				resizeCluster(4)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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
					}`, instanceIDs[1], instanceIDs[2])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the service is not EC2", func() {
			BeforeEach(func() {
				resizeCluster(4)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.health",
						"detail-type": "AWS Health Event",
						"version": "1",
						"detail": {
							"service": "INVALID",
							"eventTypeCategory": "scheduledChange",
							"affectedEntities": [
								{"entityValue": "%s"},
								{"entityValue": "%s"}
							]
						}
					}`, instanceIDs[1], instanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons and drains only the targeted nodes", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})

		When("the event type category is not scheduled change", func() {
			BeforeEach(func() {
				resizeCluster(4)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.health",
						"detail-type": "AWS Health Event",
						"version": "1",
						"detail": {
							"service": "EC2",
							"eventTypeCategory": "invalid",
							"affectedEntities": [
								{"entityValue": "%s"},
								{"entityValue": "%s"}
							]
						}
					}`, instanceIDs[1], instanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons and drains only the targeted nodes", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})
	})

	When("the SQS queue contains a Spot Interruption Notification", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("the SQS queue contains a State Change Notification", func() {
		When("the state is stopping", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, instanceIDs[1])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the state is stopped", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopped"
						}
					}`, instanceIDs[1])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the state is shutting-down", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "shutting-down"
						}
					}`, instanceIDs[1])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the state is terminated", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "terminated"
						}
					}`, instanceIDs[1])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the state is not recognized", func() {
			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "invalid"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons and drains only the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})
	})

	When("the SQS queue contains multiple messages", func() {
		BeforeEach(func() {
			resizeCluster(12)

			sqsQueues[queueURL] = append(sqsQueues[queueURL],
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
					}`, instanceIDs[1])),
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
					}`, instanceIDs[2])),
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
					}`, instanceIDs[3])),
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
					}`, instanceIDs[4], instanceIDs[5])),
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
					}`, instanceIDs[6])),
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
					}`, instanceIDs[7])),
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
					}`, instanceIDs[8])),
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
					}`, instanceIDs[9])),
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
					}`, instanceIDs[10])),
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
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("the SQS queue contains an unrecognized message", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the SQS queue contains a message with no body", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the SQS queue contains an empty message", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body:          aws.String(""),
			})
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the SQS message cannot be parsed", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the terminator has event configuration", func() {
		When("Cordon on ASG Termination v1", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on ASG Termination v1", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("Cordon on ASG Termination v2", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on ASG Termination v2", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, instanceIDs[1])),
				})

				createPendingASGLifecycleAction(instanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(asgLifecycleActions).To(And(HaveKeyWithValue(instanceIDs[1], Equal(StateComplete)), HaveLen(1)))
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Rebalance Recommendation", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.RebalanceRecommendation = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance Rebalance Recommendation",
						"version": "0",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Rebalance Recommendation", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.RebalanceRecommendation = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance Rebalance Recommendation",
						"version": "0",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Scheduled Change", func() {
			BeforeEach(func() {
				resizeCluster(4)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.ScheduledChange = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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
					}`, instanceIDs[1], instanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveKey(nodeNames[2]), HaveLen(2)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Scheduled Change", func() {
			BeforeEach(func() {
				resizeCluster(4)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.ScheduledChange = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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
					}`, instanceIDs[1], instanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Spot Interruption", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.SpotInterruption = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Spot Interruption", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.SpotInterruption = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("Cordon on State Change", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.StateChange = "Cordon"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(cordonedNodes).To(And(HaveKey(nodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on State Change", func() {
			BeforeEach(func() {
				resizeCluster(3)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Events.StateChange = "NoAction"

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
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
			receiveSQSMessageFunc = func(_ aws.Context, _ *sqs.ReceiveMessageInput, _ ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
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

	When("the terminator has webhook configuration", func() {
		const webhookURL = "http://webhook.example.aws"
		webhookHeaders := []v1alpha1.HeaderSpec{{Name: "Content-Type", Value: "application/json"}}
		webhookTemplate := fmt.Sprintf(
			`EventID={{ .EventID }}, Kind={{ .Kind }}, InstanceID={{ .InstanceID }}, NodeName={{ .NodeName }}, StartTime={{ (.StartTime.Format "%s") }}`,
			time.RFC3339,
		)

		When("the reconciliation takes no action", func() {
			BeforeEach(func() {
				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not send any webhook requests", func() {
				Expect(webhookRequests).To(BeEmpty())
			})
		})

		When("the reconciliation acts on a node", func() {
			const msgID = "id-123"
			msgTime := time.Now().Format(time.RFC3339)

			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"id": "%s",
						"time": "%s",
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, msgID, msgTime, instanceIDs[1])),
				})

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("sends a webhook notification", func() {
				Expect(webhookRequests).To(HaveLen(1))
				Expect(webhookRequests[0].Method).To(Equal(http.MethodPost))
				Expect(webhookRequests[0].URL.String()).To(Equal(webhookURL))
				Expect(webhookRequests[0].Header).To(And(
					HaveLen(1),
					HaveKeyWithValue("Content-Type", And(
						HaveLen(1),
						ContainElement("application/json"),
					))))

				Expect(ReadAll(webhookRequests[0].Body)).To(Equal(fmt.Sprintf(
					"EventID=%s, Kind=spotInterruption, InstanceID=%s, NodeName=%s, StartTime=%s",
					msgID, instanceIDs[1], nodeNames[1], msgTime,
				)))
			})
		})

		When("the reconciliation acts on multiple nodes", func() {
			msgIDs := []string{"msg-1", "msg-2", "msg-2"}
			msgBaseTime := time.Now()
			msgTimes := []string{
				msgBaseTime.Add(-1 * time.Minute).Format(time.RFC3339),
				msgBaseTime.Format(time.RFC3339),
				msgBaseTime.Format(time.RFC3339),
			}
			kinds := []string{"spotInterruption", "scheduledChange", "scheduledChange"}

			BeforeEach(func() {
				resizeCluster(5)

				sqsQueues[queueURL] = append(sqsQueues[queueURL],
					&sqs.Message{
						ReceiptHandle: aws.String("msg-1"),
						Body: aws.String(fmt.Sprintf(`{
							"id": "%s",
							"time": "%s",
							"source": "aws.ec2",
							"detail-type": "EC2 Spot Instance Interruption Warning",
							"version": "1",
							"detail": {
								"instance-id": "%s"
							}
						}`, msgIDs[0], msgTimes[0], instanceIDs[1])),
					},
					&sqs.Message{
						ReceiptHandle: aws.String("msg-1"),
						Body: aws.String(fmt.Sprintf(`{
							"id": "%s",
							"time": "%s",
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
						}`, msgIDs[1], msgTimes[1], instanceIDs[2], instanceIDs[3])),
					},
				)

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("sends a webhook notification", func() {
				Expect(webhookRequests).To(HaveLen(3))

				for i := 0; i < 3; i++ {
					Expect(webhookRequests[i].Method).To(Equal(http.MethodPost), "request #%d", i)
					Expect(webhookRequests[i].URL.String()).To(Equal(webhookURL), "request #%d", i)
					Expect(webhookRequests[i].Header).To(And(
						HaveLen(1),
						HaveKeyWithValue("Content-Type", And(
							HaveLen(1),
							ContainElement("application/json"),
						))),
						"request #%d", i)

					Expect(ReadAll(webhookRequests[i].Body)).To(Equal(fmt.Sprintf(
						"EventID=%s, Kind=%s, InstanceID=%s, NodeName=%s, StartTime=%s",
						msgIDs[i], kinds[i], instanceIDs[i+1], nodeNames[i+1], msgTimes[i],
					)),
						"request #%d", i,
					)
				}
			})
		})

		When("there is an error sending the request", func() {
			const msgID = "id-123"
			msgTime := time.Now().Format(time.RFC3339)

			BeforeEach(func() {
				resizeCluster(3)

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"id": "%s",
						"time": "%s",
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, msgID, msgTime, instanceIDs[1])),
				})

				terminator := terminators[terminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate

				webhookSendFunc = func(_ *http.Request) (*http.Response, error) {
					return nil, errors.New("test error")
				}
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})
		})
	})

	When("there is an error deleting an SQS message", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			deleteSQSMessageFunc = func(_ context.Context, _ *sqs.DeleteMessageInput, _ ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
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

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			describeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("there is no EC2 reservation for the instance ID", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			describeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the EC2 reservation contains no instances", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			describeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the EC2 reservation's instance has no PrivateDnsName", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			describeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("the EC2 reservation's instance's PrivateDnsName empty", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
			})

			describeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

		It("does not delete the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("there is an error getting the cluster node for a node name", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
		})

		It("does not cordon or drain any nodes", func() {
			Expect(cordonedNodes).To(BeEmpty())
			Expect(drainedNodes).To(BeEmpty())
		})
	})

	When("the terminator has a node label selector", func() {
		When("the label selector matches the target node", func() {
			const labelName = "a-test-label"
			const labelValue = "test-label-value"

			BeforeEach(func() {
				resizeCluster(3)

				targetedNode, found := nodes[types.NamespacedName{Name: nodeNames[1]}]
				Expect(found).To(BeTrue())

				targetedNode.Labels = map[string]string{labelName: labelValue}

				terminator, found := terminators[terminatorNamespaceName]
				Expect(found).To(BeTrue())

				terminator.Spec.MatchLabels = client.MatchingLabels{labelName: labelValue}

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
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
				Expect(sqsQueues[queueURL]).To(BeEmpty())
			})
		})

		When("the label selector does not match the target node", func() {
			const labelName = "a-test-label"
			const labelValue = "test-label-value"

			BeforeEach(func() {
				resizeCluster(3)

				terminator, found := terminators[terminatorNamespaceName]
				Expect(found).To(BeTrue())

				terminator.Spec.MatchLabels = client.MatchingLabels{labelName: labelValue}

				sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, instanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(reconciler.RequeueInterval)))
			})

			It("does not cordon or drain any nodes", func() {
				Expect(cordonedNodes).To(BeEmpty())
				Expect(drainedNodes).To(BeEmpty())
			})

			It("does not delete the message from the SQS queue", func() {
				Expect(sqsQueues[queueURL]).To(HaveLen(1))
			})
		})
	})

	When("cordoning a node fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("draining a node fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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

		It("deletes the message from the SQS queue", func() {
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("completing an ASG Lifecycle Action (v1) fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIDs[1])),
			})

			completeASGLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
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
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("the request to complete the ASG Lifecycle Action (v1) fails with a status != 400", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "1",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIDs[1])),
			})

			completeASGLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
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
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("completing an ASG Lifecycle Action (v2) fails", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "2",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIDs[1])),
			})

			completeASGLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
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
			Expect(sqsQueues[queueURL]).To(BeEmpty())
		})
	})

	When("the request to complete the ASG Lifecycle Action (v2) fails with a status != 400", func() {
		BeforeEach(func() {
			resizeCluster(3)

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.autoscaling",
					"detail-type": "EC2 Instance-terminate Lifecycle Action",
					"version": "2",
					"detail": {
						"EC2InstanceId": "%s",
						"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
					}
				}`, instanceIDs[1])),
			})

			completeASGLifecycleActionFunc = func(_ aws.Context, _ *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
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
			Expect(sqsQueues[queueURL]).To(HaveLen(1))
		})
	})

	When("getting messages from a terminator's SQS queue", func() {
		const (
			maxNumberOfMessages      = int64(10)
			visibilityTimeoutSeconds = int64(20)
			waitTimeSeconds          = int64(20)
		)
		var (
			attributeNames        = []string{sqs.MessageSystemAttributeNameSentTimestamp}
			messageAttributeNames = []string{sqs.QueueAttributeNameAll}
			input                 *sqs.ReceiveMessageInput
		)

		BeforeEach(func() {
			terminator, found := terminators[terminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.SQS.QueueURL = queueURL

			defaultReceiveSQSMessageFunc := receiveSQSMessageFunc
			receiveSQSMessageFunc = func(ctx aws.Context, in *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
				input = in
				return defaultReceiveSQSMessageFunc(ctx, in, options...)
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
			Expect(*input.QueueUrl).To(Equal(queueURL))

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

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
				ReceiptHandle: aws.String("msg-1"),
				Body: aws.String(fmt.Sprintf(`{
					"source": "aws.ec2",
					"detail-type": "EC2 Spot Instance Interruption Warning",
					"version": "1",
					"detail": {
						"instance-id": "%s"
					}
				}`, instanceIDs[1])),
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

			sqsQueues[queueURL] = append(sqsQueues[queueURL], &sqs.Message{
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
				}`, autoScalingGroupName, instanceIDs[1], lifecycleActionToken, lifecycleHookName)),
			})

			defaultCompleteASGLifecycleActionFunc := completeASGLifecycleActionFunc
			completeASGLifecycleActionFunc = func(ctx aws.Context, in *autoscaling.CompleteLifecycleActionInput, options ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
				input = in
				return defaultCompleteASGLifecycleActionFunc(ctx, in, options...)
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
			Expect(*input.InstanceId).To(Equal(instanceIDs[1]))
		})
	})

	// Setup the starter state:
	// * One terminator (terminatorNamedspacedName)
	// * The terminator references an empty sqs queue (queueURL)
	// * Zero nodes (use resizeCluster())
	//
	// Tests should modify the cluster/aws service states as needed.
	BeforeEach(func() {
		// 1. Initialize variables.

		logger := zap.New(zapcore.NewCore(
			zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
			zapcore.AddSync(io.Discard),
			zap.NewAtomicLevelAt(zap.DebugLevel),
		))

		ctx = logging.WithLogger(context.Background(), logger.Sugar())
		terminatorNamespaceName = types.NamespacedName{Namespace: "test", Name: "foo"}
		request = reconcile.Request{NamespacedName: terminatorNamespaceName}
		sqsQueues = map[SQSQueueURL][]*sqs.Message{queueURL: {}}
		terminators = map[types.NamespacedName]*v1alpha1.Terminator{
			// For convenience create a terminator that points to the sqs queue.
			terminatorNamespaceName: {
				Spec: v1alpha1.TerminatorSpec{
					SQS: v1alpha1.SQSSpec{
						QueueURL: queueURL,
					},
				},
			},
		}
		nodes = map[types.NamespacedName]*v1.Node{}
		ec2Reservations = map[EC2InstanceID]*ec2.Reservation{}
		cordonedNodes = map[NodeName]bool{}
		drainedNodes = map[NodeName]bool{}

		nodeNames = []NodeName{}
		instanceIDs = []EC2InstanceID{}
		resizeCluster = func(newNodeCount uint) {
			for currNodeCount := uint(len(nodes)); currNodeCount < newNodeCount; currNodeCount++ {
				nodeName := fmt.Sprintf("node-%d", currNodeCount)
				nodeNames = append(nodeNames, nodeName)
				nodes[types.NamespacedName{Name: nodeName}] = &v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				}

				instanceID := fmt.Sprintf("instance-%d", currNodeCount)
				instanceIDs = append(instanceIDs, instanceID)
				ec2Reservations[instanceID] = &ec2.Reservation{
					Instances: []*ec2.Instance{
						{PrivateDnsName: aws.String(nodeName)},
					},
				}
			}

			nodeNames = nodeNames[:newNodeCount]
			instanceIDs = instanceIDs[:newNodeCount]
		}

		asgLifecycleActions = map[EC2InstanceID]State{}
		createPendingASGLifecycleAction = func(instanceID EC2InstanceID) {
			Expect(asgLifecycleActions).ToNot(HaveKey(instanceID))
			asgLifecycleActions[instanceID] = StatePending
		}

		webhookRequests = []*http.Request{}
		webhookSendFunc = func(req *http.Request) (*http.Response, error) {
			webhookRequests = append(webhookRequests, req)
			return &http.Response{StatusCode: 200}, nil
		}

		// 2. Setup stub clients.

		describeEC2InstancesFunc = func(ctx aws.Context, input *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			output := ec2.DescribeInstancesOutput{}
			for _, instanceID := range input.InstanceIds {
				if instanceID == nil {
					continue
				}
				if reservation, found := ec2Reservations[*instanceID]; found {
					output.Reservations = append(output.Reservations, reservation)
				}
			}
			return &output, nil
		}

		ec2Client := EC2Client(func(ctx aws.Context, input *ec2.DescribeInstancesInput, options ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
			return describeEC2InstancesFunc(ctx, input, options...)
		})

		completeASGLifecycleActionFunc = func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, _ ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
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

		asgClient := ASGClient(func(ctx aws.Context, input *autoscaling.CompleteLifecycleActionInput, options ...awsrequest.Option) (*autoscaling.CompleteLifecycleActionOutput, error) {
			return completeASGLifecycleActionFunc(ctx, input, options...)
		})

		receiveSQSMessageFunc = func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			Expect(input.QueueUrl).ToNot(BeNil())

			messages, found := sqsQueues[*input.QueueUrl]
			Expect(found).To(BeTrue(), "SQS queue does not exist: %q", *input.QueueUrl)

			return &sqs.ReceiveMessageOutput{Messages: append([]*sqs.Message{}, messages...)}, nil
		}

		deleteSQSMessageFunc = func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
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

		sqsClient := SQSClient{
			ReceiveSQSMessageFunc: func(ctx aws.Context, input *sqs.ReceiveMessageInput, options ...awsrequest.Option) (*sqs.ReceiveMessageOutput, error) {
				return receiveSQSMessageFunc(ctx, input, options...)
			},
			DeleteSQSMessageFunc: func(ctx aws.Context, input *sqs.DeleteMessageInput, options ...awsrequest.Option) (*sqs.DeleteMessageOutput, error) {
				return deleteSQSMessageFunc(ctx, input, options...)
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

		eventParser := event.NewAggregatedParser(
			asgterminateeventv1.Parser{ASGLifecycleActionCompleter: asgClient},
			asgterminateeventv2.Parser{ASGLifecycleActionCompleter: asgClient},
			rebalancerecommendationeventv0.Parser{},
			scheduledchangeeventv1.Parser{},
			spotinterruptioneventv1.Parser{},
			statechangeeventv1.Parser{},
		)

		cordoner := kubectlcordondrain.CordonFunc(func(h *kubectl.Helper, n *v1.Node, d bool) error {
			return cordonFunc(h, n, d)
		})

		drainer := kubectlcordondrain.DrainFunc(func(h *kubectl.Helper, n string) error {
			return drainFunc(h, n)
		})

		cordonDrainerBuilder := kubectlcordondrain.Builder{
			ClientSet: &kubernetes.Clientset{},
			Cordoner:  cordoner,
			Drainer:   drainer,
		}

		newHttpClientDoFunc := func(_ webhook.ProxyFunc) webhook.HttpSendFunc {
			return webhookSendFunc
		}

		reconciler = terminator.Reconciler{
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
	})

	// Run the reconciliation before each test subject.
	JustBeforeEach(func() {
		result, err = reconciler.Reconcile(ctx, request)
	})
})

func ReadAll(r io.Reader) (string, error) {
	bs, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}
