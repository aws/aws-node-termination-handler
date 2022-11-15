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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

var _ = Describe("Reconciliation", func() {
	When("the SQS message cannot be parsed", func() {
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
				Body: aws.String(`{
					"source": "test.suite",
					"detail-type": "Mal-formed notification",
				`),
			})

			result, err = infra.Reconcile()
		})

		It("returns success and requeues the request with the reconciler's configured interval", func() {
			Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
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
