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

package sqs

import (
	awssqs "github.com/aws/aws-sdk-go/service/sqs"

	"go.uber.org/zap/zapcore"
)

type receiveMessageInputMarshaler struct {
	*awssqs.ReceiveMessageInput
}

func NewReceiveMessageInputMarshaler(input *awssqs.ReceiveMessageInput) zapcore.ObjectMarshaler {
	return receiveMessageInputMarshaler{ReceiveMessageInput: input}
}

func (r receiveMessageInputMarshaler) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	if r.ReceiveMessageInput == nil {
		return nil
	}

	if r.QueueUrl != nil {
		enc.AddString("queueUrl", *r.QueueUrl)
	}
	if r.ReceiveRequestAttemptId != nil {
		enc.AddString("receiveAttemptId", *r.ReceiveRequestAttemptId)
	}
	if r.VisibilityTimeout != nil {
		enc.AddInt64("visibilityTimeout", *r.VisibilityTimeout)
	}
	if r.WaitTimeSeconds != nil {
		enc.AddInt64("waitTimeSeconds", *r.WaitTimeSeconds)
	}
	if r.MaxNumberOfMessages != nil {
		enc.AddInt64("maxNumberOfMessages", *r.MaxNumberOfMessages)
	}
	enc.AddArray("attributeNames", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, attr := range r.AttributeNames {
			if attr != nil {
				enc.AppendString(*attr)
			}
		}
		return nil
	}))
	enc.AddArray("messageAttributeNames", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, attr := range r.MessageAttributeNames {
			if attr != nil {
				enc.AppendString(*attr)
			}
		}
		return nil
	}))
	return nil
}
