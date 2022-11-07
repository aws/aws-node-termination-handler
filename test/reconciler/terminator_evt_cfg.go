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

package reconciler

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

var _ = Describe("Reconciliation", func() {
	When("the terminator has event configuration", func() {
		var (
			infra  *mock.Infrastructure
			result reconcile.Result
			err    error
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
		})

		JustBeforeEach(func() {
			result, err = infra.Reconcile()
		})

		When("Cordon on ASG Termination v1", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, infra.InstanceIDs[1])),
				})

				infra.CreatePendingASGLifecycleAction(infra.InstanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(infra.ASGLifecycleActions).To(
					SatisfyAll(
						HaveKeyWithValue(infra.InstanceIDs[1], Equal(mock.StateComplete)),
						HaveLen(1),
					),
				)
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on ASG Termination v1", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "1",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, infra.InstanceIDs[1])),
				})

				infra.CreatePendingASGLifecycleAction(infra.InstanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(infra.ASGLifecycleActions).To(
					SatisfyAll(
						HaveKeyWithValue(infra.InstanceIDs[1], Equal(mock.StateComplete)),
						HaveLen(1),
					),
				)
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("Cordon on ASG Termination v2", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, infra.InstanceIDs[1])),
				})

				infra.CreatePendingASGLifecycleAction(infra.InstanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(infra.ASGLifecycleActions).To(
					SatisfyAll(
						HaveKeyWithValue(infra.InstanceIDs[1], Equal(mock.StateComplete)),
						HaveLen(1),
					),
				)
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on ASG Termination v2", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.AutoScalingTermination = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.autoscaling",
						"detail-type": "EC2 Instance-terminate Lifecycle Action",
						"version": "2",
						"detail": {
							"EC2InstanceId": "%s",
							"LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
						}
					}`, infra.InstanceIDs[1])),
				})

				infra.CreatePendingASGLifecycleAction(infra.InstanceIDs[1])
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("completes the ASG lifecycle action", func() {
				Expect(infra.ASGLifecycleActions).To(
					SatisfyAll(
						HaveKeyWithValue(infra.InstanceIDs[1], Equal(mock.StateComplete)),
						HaveLen(1),
					),
				)
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Rebalance Recommendation", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.RebalanceRecommendation = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance Rebalance Recommendation",
						"version": "0",
						"detail": {
							"instance-id": "%s"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Rebalance Recommendation", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.RebalanceRecommendation = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance Rebalance Recommendation",
						"version": "0",
						"detail": {
							"instance-id": "%s"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Scheduled Change", func() {
			BeforeEach(func() {
				infra.ResizeCluster(4)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.ScheduledChange = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
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
					}`, infra.InstanceIDs[1], infra.InstanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(
					SatisfyAll(
						HaveKey(infra.NodeNames[1]),
						HaveKey(infra.NodeNames[2]),
						HaveLen(2),
					),
				)
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Scheduled Change", func() {
			BeforeEach(func() {
				infra.ResizeCluster(4)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.ScheduledChange = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
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
					}`, infra.InstanceIDs[1], infra.InstanceIDs[2])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("Cordon on Spot Interruption", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.SpotInterruption = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(
					SatisfyAll(
						HaveKey(infra.NodeNames[1]),
						HaveLen(1),
					),
				)
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on Spot Interruption", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.SpotInterruption = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Spot Instance Interruption Warning",
						"version": "1",
						"detail": {
							"instance-id": "%s"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("Cordon on State Change", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.StateChange = "Cordon"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("cordons the targeted node", func() {
				Expect(infra.CordonedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
			})

			It("does not drain the targeted node", func() {
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})

		When("\"No Action\" on State Change", func() {
			BeforeEach(func() {
				infra.ResizeCluster(3)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Events.StateChange = "NoAction"

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
					ReceiptHandle: aws.String("msg-1"),
					Body: aws.String(fmt.Sprintf(`{
						"source": "aws.ec2",
						"detail-type": "EC2 Instance State-change Notification",
						"version": "1",
						"detail": {
							"instance-id": "%s",
							"state": "stopping"
						}
					}`, infra.InstanceIDs[1])),
				})
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not cordon or drain the targeted node", func() {
				Expect(infra.CordonedNodes).To(BeEmpty())
				Expect(infra.DrainedNodes).To(BeEmpty())
			})

			It("deletes the message from the SQS queue", func() {
				Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
			})
		})
	})
})
