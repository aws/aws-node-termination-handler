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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var _ = Describe("Reconciliation", func() {
	When("getting messages from a terminator's SQS queue", func() {
		const (
			maxNumberOfMessages      = int32(10)
			visibilityTimeoutSeconds = int32(20)
			waitTimeSeconds          = int32(20)
		)
		var (
			attributeNames        = []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll}
			messageAttributeNames = []string{string(sqstypes.MessageSystemAttributeNameSentTimestamp)}
			input                 *sqs.ReceiveMessageInput
			infra                 *mock.Infrastructure
		)

		BeforeEach(func() {
			infra = mock.NewInfrastructure()
			terminator, found := infra.Terminators[infra.TerminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.SQS.QueueURL = mock.QueueURL

			defaultReceiveSQSMessageFunc := infra.ReceiveSQSMessageFunc
			infra.ReceiveSQSMessageFunc = func(ctx context.Context, in *sqs.ReceiveMessageInput, options ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
				input = in
				return defaultReceiveSQSMessageFunc(ctx, in, options...)
			}

			infra.Reconcile()
		})

		It("sends the input values from the terminator", func() {
			Expect(input).ToNot(BeNil())

			for i, attrName := range input.AttributeNames {
				Expect(attrName).ToNot(BeNil())
				Expect(attrName).To(Equal(attributeNames[i]))
			}
			for i, attrName := range input.MessageAttributeNames {
				Expect(attrName).ToNot(BeNil())
				Expect(attrName).To(Equal(messageAttributeNames[i]))
			}

			Expect(input.MaxNumberOfMessages).ToNot(BeNil())
			Expect(input.MaxNumberOfMessages).To(Equal(maxNumberOfMessages))

			Expect(input.QueueUrl).ToNot(BeNil())
			Expect(*input.QueueUrl).To(Equal(mock.QueueURL))

			Expect(input.VisibilityTimeout).ToNot(BeNil())
			Expect(input.VisibilityTimeout).To(Equal(visibilityTimeoutSeconds))

			Expect(input.WaitTimeSeconds).ToNot(BeNil())
			Expect(input.WaitTimeSeconds).To(Equal(waitTimeSeconds))
		})
	})
})
