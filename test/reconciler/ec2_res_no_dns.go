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
	awsrequest "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
)

var _ = Describe("Reconciliation", func() {
	When("the EC2 reservation's instance has no PrivateDnsName", func() {
		var (
			infra  *mock.Infrastructure
			result reconcile.Result
			err    error
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
			infra.ResizeCluster(3)

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

			infra.DescribeEC2InstancesFunc = func(_ aws.Context, _ *ec2.DescribeInstancesInput, _ ...awsrequest.Option) (*ec2.DescribeInstancesOutput, error) {
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

			result, err = infra.Reconcile()
		})

		It("does not requeue the request", func() {
			Expect(result).To(BeZero())
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("does not cordon or drain any nodes", func() {
			Expect(infra.CordonedNodes).To(BeEmpty())
			Expect(infra.DrainedNodes).To(BeEmpty())
		})

		It("does not delete the message from the SQS queue", func() {
			Expect(infra.SQSQueues[mock.QueueURL]).To(HaveLen(1))
		})
	})
})
