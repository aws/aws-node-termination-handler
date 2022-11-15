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
	When("the SQS queue contains multiple messages", func() {
		var (
			infra  *mock.Infrastructure
			result reconcile.Result
			err    error
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
			infra.ResizeCluster(12)

			infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL],
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
					}`, infra.InstanceIDs[1])),
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
					}`, infra.InstanceIDs[2])),
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
					}`, infra.InstanceIDs[3])),
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
					}`, infra.InstanceIDs[4], infra.InstanceIDs[5])),
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
					}`, infra.InstanceIDs[6])),
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
					}`, infra.InstanceIDs[7])),
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
					}`, infra.InstanceIDs[8])),
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
					}`, infra.InstanceIDs[9])),
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
					}`, infra.InstanceIDs[10])),
				},
			)

			result, err = infra.Reconcile()
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted nodes", func() {
			Expect(infra.CordonedNodes).To(SatisfyAll(
				HaveKey(infra.NodeNames[1]),
				HaveKey(infra.NodeNames[2]),
				HaveKey(infra.NodeNames[3]),
				HaveKey(infra.NodeNames[4]),
				HaveKey(infra.NodeNames[5]),
				HaveKey(infra.NodeNames[6]),
				HaveKey(infra.NodeNames[7]),
				HaveKey(infra.NodeNames[8]),
				HaveKey(infra.NodeNames[9]),
				HaveKey(infra.NodeNames[10]),
				HaveLen(10),
			))
			Expect(infra.DrainedNodes).To(SatisfyAll(
				HaveKey(infra.NodeNames[1]),
				HaveKey(infra.NodeNames[2]),
				HaveKey(infra.NodeNames[3]),
				HaveKey(infra.NodeNames[4]),
				HaveKey(infra.NodeNames[5]),
				HaveKey(infra.NodeNames[6]),
				HaveKey(infra.NodeNames[7]),
				HaveKey(infra.NodeNames[8]),
				HaveKey(infra.NodeNames[9]),
				HaveKey(infra.NodeNames[10]),
				HaveLen(10),
			))
		})

		It("deletes the messages from the SQS queue", func() {
			Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
		})
	})
})
