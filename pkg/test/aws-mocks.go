// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License

package test

import (
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
)

// MockedSQS mocks the SQS API
type MockedSQS struct {
	sqsiface.SQSAPI
	ReceiveMessageResp sqs.ReceiveMessageOutput
	ReceiveMessageErr  error
	DeleteMessageResp  sqs.DeleteMessageOutput
	DeleteMessageErr   error
}

// ReceiveMessage mocks the sqs.ReceiveMessage API call
func (m MockedSQS) ReceiveMessage(input *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	return &m.ReceiveMessageResp, m.ReceiveMessageErr
}

// DeleteMessage mocks the sqs.DeleteMessage API call
func (m MockedSQS) DeleteMessage(input *sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error) {
	return &m.DeleteMessageResp, m.DeleteMessageErr
}

// MockedEC2 mocks the EC2 API
type MockedEC2 struct {
	ec2iface.EC2API
	DescribeInstancesResp ec2.DescribeInstancesOutput
	DescribeInstancesErr  error
}

// DescribeInstances mocks the ec2.DescribeInstances API call
func (m MockedEC2) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return &m.DescribeInstancesResp, m.DescribeInstancesErr
}

// MockedASG mocks the autoscaling API
type MockedASG struct {
	autoscalingiface.AutoScalingAPI
	CompleteLifecycleActionResp      autoscaling.CompleteLifecycleActionOutput
	CompleteLifecycleActionErr       error
	DescribeAutoScalingInstancesResp autoscaling.DescribeAutoScalingInstancesOutput
	DescribeAutoScalingInstancesErr  error
	DescribeTagsPagesResp            autoscaling.DescribeTagsOutput
	DescribeTagsPagesErr             error
}

// CompleteLifecycleAction mocks the autoscaling.CompleteLifecycleAction API call
func (m MockedASG) CompleteLifecycleAction(input *autoscaling.CompleteLifecycleActionInput) (*autoscaling.CompleteLifecycleActionOutput, error) {
	return &m.CompleteLifecycleActionResp, m.CompleteLifecycleActionErr
}

// DescribeAutoScalingInstances mocks the autoscaling.DescribeAutoScalingInstances API call
func (m MockedASG) DescribeAutoScalingInstances(input *autoscaling.DescribeAutoScalingInstancesInput) (*autoscaling.DescribeAutoScalingInstancesOutput, error) {
	return &m.DescribeAutoScalingInstancesResp, m.DescribeAutoScalingInstancesErr
}

type describeTagsPagesFn = func(page *autoscaling.DescribeTagsOutput, lastPage bool) bool

// DescribeTagsPages mocks the autoscaling.DescribeTagsPages API call
func (m MockedASG) DescribeTagsPages(input *autoscaling.DescribeTagsInput, fn describeTagsPagesFn) error {
	fn(&m.DescribeTagsPagesResp, true)
	return m.DescribeTagsPagesErr
}
