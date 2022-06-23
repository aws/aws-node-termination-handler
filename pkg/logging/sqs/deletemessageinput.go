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

package sqs

import (
	awssqs "github.com/aws/aws-sdk-go/service/sqs"

	"go.uber.org/zap/zapcore"
)

type deleteMessageInputMarshaler struct {
	*awssqs.DeleteMessageInput
}

func NewDeleteMessageInputMarshaler(input *awssqs.DeleteMessageInput) zapcore.ObjectMarshaler {
	return deleteMessageInputMarshaler{DeleteMessageInput: input}
}

func (d deleteMessageInputMarshaler) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	if d.DeleteMessageInput == nil {
		return nil
	}

	if d.QueueUrl != nil {
		enc.AddString("queueUrl", *d.QueueUrl)
	}
	if d.ReceiptHandle != nil {
		enc.AddString("receiptHandle", *d.ReceiptHandle)
	}
	return nil
}
