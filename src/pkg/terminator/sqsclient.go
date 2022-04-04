/*
Copyright 2022.

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

package terminator

import (
	"context"
	"fmt"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type (
	SQSMessageClient interface {
		GetSQSMessages(context.Context, *sqs.ReceiveMessageInput) ([]*sqs.Message, error)
		DeleteSQSMessage(context.Context, *sqs.DeleteMessageInput) error
	}

	sqsMessageClientAdapterBuilder struct {
		SQSMessageClient
	}

	sqsMessageClientAdapter struct {
		sqs.DeleteMessageInput
		sqs.ReceiveMessageInput
		SQSMessageClient
	}
)

func NewSQSClientBuilder(client SQSMessageClient) (SQSClientBuilder, error) {
	if client == nil {
		return nil, fmt.Errorf("argument 'client' is nil")
	}
	return sqsMessageClientAdapterBuilder{SQSMessageClient: client}, nil
}

func (s sqsMessageClientAdapterBuilder) NewSQSClient(terminator *v1alpha1.Terminator) (SQSClient, error) {
	if terminator == nil {
		return nil, fmt.Errorf("argument 'terminator' is nil")
	}

	receiveMessageInput := sqs.ReceiveMessageInput{
		MaxNumberOfMessages: aws.Int64(terminator.Spec.SQS.MaxNumberOfMessages),
		QueueUrl:            aws.String(terminator.Spec.SQS.QueueURL),
		VisibilityTimeout:   aws.Int64(terminator.Spec.SQS.VisibilityTimeoutSeconds),
		WaitTimeSeconds:     aws.Int64(terminator.Spec.SQS.WaitTimeSeconds),
	}
	receiveMessageInput.AttributeNames = make([]*string, len(terminator.Spec.SQS.AttributeNames))
	for i, attrName := range terminator.Spec.SQS.AttributeNames {
		receiveMessageInput.AttributeNames[i] = aws.String(attrName)
	}
	receiveMessageInput.MessageAttributeNames = make([]*string, len(terminator.Spec.SQS.MessageAttributeNames))
	for i, attrName := range terminator.Spec.SQS.MessageAttributeNames {
		receiveMessageInput.MessageAttributeNames[i] = aws.String(attrName)
	}

	deleteMessageInput := sqs.DeleteMessageInput{
		QueueUrl: aws.String(terminator.Spec.SQS.QueueURL),
	}

	return sqsMessageClientAdapter{
		DeleteMessageInput:  deleteMessageInput,
		ReceiveMessageInput: receiveMessageInput,
		SQSMessageClient:    s.SQSMessageClient,
	}, nil
}

func (a sqsMessageClientAdapter) GetSQSMessages(ctx context.Context) ([]*sqs.Message, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).
		With("params", logging.NewReceiveMessageInputMarshaler(&a.ReceiveMessageInput)),
	)

	return a.SQSMessageClient.GetSQSMessages(ctx, &a.ReceiveMessageInput)
}

func (a sqsMessageClientAdapter) DeleteSQSMessage(ctx context.Context, msg *sqs.Message) error {
	a.DeleteMessageInput.ReceiptHandle = msg.ReceiptHandle

	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).
		With("params", logging.NewDeleteMessageInputMarshaler(&a.DeleteMessageInput)),
	)

	return a.SQSMessageClient.DeleteSQSMessage(ctx, &a.DeleteMessageInput)
}
