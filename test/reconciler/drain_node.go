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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubectl "k8s.io/kubectl/pkg/drain"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var _ = Describe("Reconciliation", func() {
	When("draining a node", func() {
		const (
			force               = true
			gracePeriodSeconds  = 31
			ignoreAllDaemonSets = true
			deleteEmptyDirData  = true
		)
		var (
			infra   *mock.Infrastructure
			helper  *kubectl.Helper
			timeout time.Duration
		)

		BeforeEach(func() {
			timeout = 42 * time.Second

			infra = mock.NewInfrastructure()
			terminator, found := infra.Terminators[infra.TerminatorNamespaceName]
			Expect(found).To(BeTrue())

			terminator.Spec.Drain.DeleteEmptyDirData = deleteEmptyDirData
			terminator.Spec.Drain.Force = force
			terminator.Spec.Drain.GracePeriodSeconds = gracePeriodSeconds
			terminator.Spec.Drain.IgnoreAllDaemonSets = ignoreAllDaemonSets
			terminator.Spec.Drain.TimeoutSeconds = int(timeout.Seconds())

			defaultDrainFunc := infra.DrainFunc
			infra.DrainFunc = func(h *kubectl.Helper, nodeName string) error {
				helper = h
				return defaultDrainFunc(h, nodeName)
			}

			infra.ResizeCluster(3)

			infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], sqstypes.Message{
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

			infra.Reconcile()
		})

		It("sends the input values from the terminator", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(SatisfyAll(
				HaveField("DeleteEmptyDirData", Equal(deleteEmptyDirData)),
				HaveField("Force", Equal(force)),
				HaveField("GracePeriodSeconds", Equal(gracePeriodSeconds)),
				HaveField("IgnoreAllDaemonSets", Equal(ignoreAllDaemonSets)),
				HaveField("Timeout", Equal(timeout)),
			))
		})

		It("sends additional values", func() {
			Expect(helper).ToNot(BeNil())

			Expect(helper).To(SatisfyAll(
				HaveField("Client", Not(BeNil())),
				HaveField("Ctx", Not(BeNil())),
				HaveField("Out", Not(BeNil())),
				HaveField("ErrOut", Not(BeNil())),
			))
		})
	})
})
