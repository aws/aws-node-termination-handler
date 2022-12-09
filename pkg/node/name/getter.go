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

package name

import (
	"context"
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type (
	EC2InstancesDescriber interface {
		DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	}

	Getter struct {
		EC2InstancesDescriber
	}
)

func (g Getter) GetNodeName(ctx context.Context, instanceID string) (string, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("nodeName"))

	result, err := g.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("DescribeInstances API call failed")
		return "", err
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		err = fmt.Errorf("no EC2 reservation for instance ID %s", instanceID)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance not found")
		return "", err
	}

	if result.Reservations[0].Instances[0].PrivateDnsName == nil {
		err = fmt.Errorf("no PrivateDnsName for instance %s", instanceID)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance has no PrivateDnsName")
		return "", err
	}

	nodeName := *result.Reservations[0].Instances[0].PrivateDnsName
	if nodeName == "" {
		err = fmt.Errorf("empty PrivateDnsName for instance %s", instanceID)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance's PrivateDnsName is empty")
		return "", err
	}

	return nodeName, nil
}
