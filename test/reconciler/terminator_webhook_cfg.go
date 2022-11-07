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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"

	"github.com/aws/aws-node-termination-handler/test/reconciler/mock"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

var _ = Describe("Reconciliation", func() {
	When("the terminator has webhook configuration", func() {
		const webhookURL = "http://webhook.example.aws"
		var (
			webhookHeaders  = []v1alpha1.HeaderSpec{{Name: "Content-Type", Value: "application/json"}}
			webhookTemplate = fmt.Sprintf(
				`EventID={{ .EventID }}, Kind={{ .Kind }}, InstanceID={{ .InstanceID }}, NodeName={{ .NodeName }}, StartTime={{ (.StartTime.Format "%s") }}`,
				time.RFC3339,
			)

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

		When("the reconciliation takes no action", func() {
			BeforeEach(func() {
				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("does not send any webhook requests", func() {
				Expect(infra.WebhookRequests).To(BeEmpty())
			})
		})

		When("the reconciliation acts on a node", func() {
			const msgID = "id-123"
			msgTime := time.Now().Format(time.RFC3339)

			BeforeEach(func() {
				infra.ResizeCluster(3)

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
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
					}`, msgID, msgTime, infra.InstanceIDs[1])),
				})

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("sends a webhook notification", func() {
				Expect(infra.WebhookRequests).To(HaveLen(1))
				Expect(infra.WebhookRequests[0].Method).To(Equal(http.MethodPost))
				Expect(infra.WebhookRequests[0].URL.String()).To(Equal(webhookURL))
				Expect(infra.WebhookRequests[0].Header).To(SatisfyAll(
					HaveLen(1),
					HaveKeyWithValue("Content-Type", SatisfyAll(
						HaveLen(1),
						ContainElement("application/json"),
					))))

				Expect(ReadAll(infra.WebhookRequests[0].Body)).To(Equal(fmt.Sprintf(
					"EventID=%s, Kind=spotInterruption, InstanceID=%s, NodeName=%s, StartTime=%s",
					msgID, infra.InstanceIDs[1], infra.NodeNames[1], msgTime,
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
				infra.ResizeCluster(5)

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL],
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
						}`, msgIDs[0], msgTimes[0], infra.InstanceIDs[1])),
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
						}`, msgIDs[1], msgTimes[1], infra.InstanceIDs[2], infra.InstanceIDs[3])),
					},
				)

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})

			It("sends a webhook notification", func() {
				Expect(infra.WebhookRequests).To(HaveLen(3))

				for i := 0; i < 3; i++ {
					Expect(infra.WebhookRequests[i].Method).To(Equal(http.MethodPost), "request #%d", i)
					Expect(infra.WebhookRequests[i].URL.String()).To(Equal(webhookURL), "request #%d", i)
					Expect(infra.WebhookRequests[i].Header).To(SatisfyAll(
						HaveLen(1),
						HaveKeyWithValue("Content-Type", SatisfyAll(
							HaveLen(1),
							ContainElement("application/json"),
						))),
						"request #%d", i)

					Expect(ReadAll(infra.WebhookRequests[i].Body)).To(Equal(fmt.Sprintf(
						"EventID=%s, Kind=%s, InstanceID=%s, NodeName=%s, StartTime=%s",
						msgIDs[i], kinds[i], infra.InstanceIDs[i+1], infra.NodeNames[i+1], msgTimes[i],
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
				infra.ResizeCluster(3)

				infra.SQSQueues[mock.QueueURL] = append(infra.SQSQueues[mock.QueueURL], &sqs.Message{
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
					}`, msgID, msgTime, infra.InstanceIDs[1])),
				})

				terminator := infra.Terminators[infra.TerminatorNamespaceName]
				terminator.Spec.Webhook.URL = webhookURL
				terminator.Spec.Webhook.Headers = webhookHeaders
				terminator.Spec.Webhook.Template = webhookTemplate

				infra.WebhookSendFunc = func(_ *http.Request) (*http.Response, error) {
					return nil, errors.New("test error")
				}
			})

			It("returns success and requeues the request with the reconciler's configured interval", func() {
				Expect(result, err).To(HaveField("RequeueAfter", Equal(infra.Reconciler.RequeueInterval)))
			})
		})
	})
})

func ReadAll(r io.Reader) (string, error) {
	bs, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}
