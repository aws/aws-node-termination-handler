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

package name

import (
	"context"
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type (
	EC2InstancesDescriber interface {
		DescribeInstancesWithContext(aws.Context, *ec2.DescribeInstancesInput, ...request.Option) (*ec2.DescribeInstancesOutput, error)
	}

	Getter interface {
		GetNodeName(context.Context, string) (string, error)
	}

	getter struct {
		EC2InstancesDescriber
	}
)

func NewGetter(describer EC2InstancesDescriber) (Getter, error) {
	if describer == nil {
		return nil, fmt.Errorf("argument 'describer' is nil")
	}
	return getter{EC2InstancesDescriber: describer}, nil
}

func (n getter) GetNodeName(ctx context.Context, instanceId string) (string, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("nodeName"))

	result, err := n.DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(instanceId)},
	})
	if err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("DescribeInstances API call failed")
		return "", err
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		err = fmt.Errorf("no EC2 reservation for instance ID %s", instanceId)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance not found")
		return "", err
	}

	if result.Reservations[0].Instances[0].PrivateDnsName == nil {
		err = fmt.Errorf("no PrivateDnsName for instance %s", instanceId)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance has no PrivateDnsName")
		return "", err
	}

	nodeName := *result.Reservations[0].Instances[0].PrivateDnsName
	if nodeName == "" {
		err = fmt.Errorf("empty PrivateDnsName for instance %s", instanceId)
		logging.FromContext(ctx).
			With("error", err).
			Error("EC2 instance's PrivateDnsName is empty")
		return "", err
	}

	return nodeName, nil
}
