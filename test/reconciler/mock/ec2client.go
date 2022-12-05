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

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type (
	DescribeEC2InstancesFunc = func(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)

	EC2Client DescribeEC2InstancesFunc
)

func (e EC2Client) DescribeInstances(ctx context.Context, input *ec2.DescribeInstancesInput, options ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return e(ctx, input, options...)
}
