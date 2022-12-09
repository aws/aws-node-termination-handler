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

package mock

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type (
	ReceiveSQSMessageFunc = func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteSQSMessageFunc  = func(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)

	SQSClient struct {
		ReceiveSQSMessageFunc
		DeleteSQSMessageFunc
	}
)

func (s SQSClient) ReceiveMessage(ctx context.Context, input *sqs.ReceiveMessageInput, options ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return s.ReceiveSQSMessageFunc(ctx, input, options...)
}

func (s SQSClient) DeleteMessage(ctx context.Context, input *sqs.DeleteMessageInput, options ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return s.DeleteSQSMessageFunc(ctx, input, options...)
}
