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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var _ = Describe("Reconciliation", func() {
	When("completing an ASG Complete Lifecycle Action", func() {
		const (
			autoScalingGroupName  = "testAutoScalingGroupName"
			lifecycleActionResult = "CONTINUE"
			lifecycleHookName     = "testLifecycleHookName"
			lifecycleActionToken  = "testLifecycleActionToken"
		)
		var (
			infra *mock.Infrastructure
			input *autoscaling.CompleteLifecycleActionInput
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
			infra.ResizeCluster(3)

			infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], sqstypes.Message{
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
				}`, autoScalingGroupName, infra.InstanceIDs[1], lifecycleActionToken, lifecycleHookName)),
			})

			defaultCompleteASGLifecycleActionFunc := infra.CompleteASGLifecycleActionFunc
			infra.CompleteASGLifecycleActionFunc = func(ctx context.Context, in *autoscaling.CompleteLifecycleActionInput, options ...func(*autoscaling.Options)) (*autoscaling.CompleteLifecycleActionOutput, error) {
				input = in
				return defaultCompleteASGLifecycleActionFunc(ctx, in, options...)
			}

			infra.Reconcile()
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
			Expect(*input.InstanceId).To(Equal(infra.InstanceIDs[1]))
		})
	})
})
