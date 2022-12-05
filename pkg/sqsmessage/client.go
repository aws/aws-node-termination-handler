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

package sqsmessage

import (
	"context"

	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type (
	SQSClient interface {
		ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
		DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	}

	Client struct {
		SQSClient
	}
)

func (c Client) GetSQSMessages(ctx context.Context, params *sqs.ReceiveMessageInput) ([]sqstypes.Message, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("sqsClient.getMessages"))

	result, err := c.ReceiveMessage(ctx, params)
	if err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to fetch messages")
		return nil, err
	}

	return result.Messages, nil
}

func (c Client) DeleteSQSMessage(ctx context.Context, params *sqs.DeleteMessageInput) error {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("sqsClient.deleteMessage"))

	_, err := c.DeleteMessage(ctx, params)
	if err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to delete message")
		return err
	}

	return nil
}
