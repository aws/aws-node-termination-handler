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

package adapter

import (
	"context"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type (
	SQSMessageClient interface {
		GetSQSMessages(context.Context, *sqs.ReceiveMessageInput) ([]sqstypes.Message, error)
		DeleteSQSMessage(context.Context, *sqs.DeleteMessageInput) error
	}

	sqsMessageClient struct {
		sqs.DeleteMessageInput
		sqs.ReceiveMessageInput
		SQSMessageClient
	}

	SQSMessageClientBuilder struct {
		SQSMessageClient
	}
)

func (s SQSMessageClientBuilder) NewSQSClient(terminator *v1alpha1.Terminator) (terminator.SQSClient, error) {
	receiveMessageInput := sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(terminator.Spec.SQS.QueueURL),
		MaxNumberOfMessages:   10,
		VisibilityTimeout:     20, // Seconds
		WaitTimeSeconds:       20, // Seconds, maximum for long polling
		AttributeNames:        []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
		MessageAttributeNames: []string{string(sqstypes.MessageSystemAttributeNameSentTimestamp)},
	}

	deleteMessageInput := sqs.DeleteMessageInput{
		QueueUrl: aws.String(terminator.Spec.SQS.QueueURL),
	}

	return sqsMessageClient{
		DeleteMessageInput:  deleteMessageInput,
		ReceiveMessageInput: receiveMessageInput,
		SQSMessageClient:    s.SQSMessageClient,
	}, nil
}

func (s sqsMessageClient) GetSQSMessages(ctx context.Context) ([]sqstypes.Message, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).
		With("params", logging.NewReceiveMessageInputMarshaler(&s.ReceiveMessageInput)),
	)

	return s.SQSMessageClient.GetSQSMessages(ctx, &s.ReceiveMessageInput)
}

func (s sqsMessageClient) DeleteSQSMessage(ctx context.Context, msg *sqstypes.Message) error {
	s.DeleteMessageInput.ReceiptHandle = msg.ReceiptHandle

	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).
		With("params", logging.NewDeleteMessageInputMarshaler(s.DeleteMessageInput)),
	)

	return s.SQSMessageClient.DeleteSQSMessage(ctx, &s.DeleteMessageInput)
}
