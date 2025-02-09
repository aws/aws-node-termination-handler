// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
// permissions and limitations under the License.

package ec2helper

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type IEC2Helper interface {
	GetInstanceIdsMapByTagKey(tag string) (map[string]bool, error)
}

type EC2Helper struct {
	ec2ServiceClient ec2iface.EC2API
}

func New(ec2 ec2iface.EC2API) EC2Helper {
	return EC2Helper{
		ec2ServiceClient: ec2,
	}
}

func (h EC2Helper) GetInstanceIdsByTagKey(tag string) ([]string, error) {
	ids := []string{}
	var nextToken string

	for {
		result, err := h.ec2ServiceClient.DescribeInstances(&ec2.DescribeInstancesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("tag-key"),
					Values: []*string{aws.String(tag)},
				},
			},
			NextToken: &nextToken,
		})

		if err != nil {
			return nil, err
		}

		if result == nil || result.Reservations == nil {
			return nil, fmt.Errorf("describe instances success but return empty response for tag key: %s", tag)
		}

		for _, reservation := range result.Reservations {
			if reservation.Instances == nil {
				continue
			}
			for _, instance := range reservation.Instances {
				if instance == nil || instance.InstanceId == nil {
					continue
				}
				ids = append(ids, *instance.InstanceId)
			}
		}

		if result.NextToken == nil {
			break
		}
		nextToken = *result.NextToken
	}

	return ids, nil
}

func (h EC2Helper) GetInstanceIdsMapByTagKey(tag string) (map[string]bool, error) {
	idMap := map[string]bool{}
	ids, err := h.GetInstanceIdsByTagKey(tag)
	if err != nil {
		return nil, err
	}

	if ids == nil {
		return nil, fmt.Errorf("get instance ids success but return empty response for tag key: %s", tag)
	}

	for _, id := range ids {
		idMap[id] = true
	}

	return idMap, nil
}
