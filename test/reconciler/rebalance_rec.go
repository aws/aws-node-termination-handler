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

	"github.com/aws/aws-sdk-go-v2/aws"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var _ = Describe("Reconciliation", func() {
	When("the SQS queue contains a Rebalance Recommendation Notification", func() {
		var (
			infra  *mock.Infrastructure
			result reconcile.Result
			err    error
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
			infra.ResizeCluster(3)

			infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], sqstypes.Message{
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

			result, err = infra.Reconcile()
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
		})

		It("cordons and drains only the targeted node", func() {
			Expect(infra.CordonedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
			Expect(infra.DrainedNodes).To(SatisfyAll(HaveKey(infra.NodeNames[1]), HaveLen(1)))
		})

		It("deletes the message from the SQS queue", func() {
			Expect(infra.SQSQueues[mock.QueueURL]).To(BeEmpty())
		})
	})
})