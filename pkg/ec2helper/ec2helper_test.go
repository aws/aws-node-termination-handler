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

package ec2helper_test

import (
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/ec2helper"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	instanceId1 = "i-1"
	instanceId2 = "i-2"
)

func TestGetInstanceIdsByTagKey(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(),
	}
	ec2Helper := ec2helper.New(ec2Mock)
	instanceIds, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)

	h.Equals(t, 2, len(instanceIds))
	h.Equals(t, instanceId1, instanceIds[0])
	h.Equals(t, instanceId2, instanceIds[1])
}

func TestGetInstanceIdsByTagKeyAPIError(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(),
		DescribeInstancesErr:  awserr.New("ThrottlingException", "Rate exceeded", nil),
	}
	ec2Helper := ec2helper.New(ec2Mock)
	_, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Nok(t, err)
}

func TestGetInstanceIdsByTagKeyNilResponse(t *testing.T) {
	ec2Mock := h.MockedEC2{}
	ec2Helper := ec2helper.New(ec2Mock)
	_, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Nok(t, err)
}

func TestGetInstanceIdsByTagKeyNilReservations(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: ec2.DescribeInstancesOutput{
			Reservations: nil,
		},
	}
	ec2Helper := ec2helper.New(ec2Mock)
	_, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Nok(t, err)
}

func TestGetInstanceIdsByTagKeyEmptyReservation(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{},
		},
	}
	ec2Helper := ec2helper.New(ec2Mock)
	instanceIds, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)
	h.Equals(t, 0, len(instanceIds))
}

func TestGetInstanceIdsByTagKeyEmptyInstances(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: []*ec2.Instance{},
				},
			},
		},
	}
	ec2Helper := ec2helper.New(ec2Mock)
	instanceIds, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)
	h.Equals(t, 0, len(instanceIds))
}

func TestGetInstanceIdsByTagKeyNilInstancesId(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: []*ec2.Instance{
						{
							InstanceId: nil,
						},
						{
							InstanceId: aws.String(instanceId1),
						},
					},
				},
			},
		},
	}
	ec2Helper := ec2helper.New(ec2Mock)
	instanceIds, err := ec2Helper.GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)
	h.Equals(t, 1, len(instanceIds))
}

func TestGetInstanceIdsMapByTagKey(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(),
	}
	ec2Helper := ec2helper.New(ec2Mock)
	instanceIdsMap, err := ec2Helper.GetInstanceIdsMapByTagKey("myNTHManagedTag")
	h.Ok(t, err)

	_, exist := instanceIdsMap[instanceId1]
	h.Equals(t, true, exist)
	_, exist = instanceIdsMap[instanceId2]
	h.Equals(t, true, exist)
	_, exist = instanceIdsMap["non-existent instance id"]
	h.Equals(t, false, exist)
}

func getDescribeInstancesResp() ec2.DescribeInstancesOutput {
	return ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: []*ec2.Instance{
					{
						InstanceId: aws.String(instanceId1),
					},
					{
						InstanceId: aws.String(instanceId2),
					},
				},
			},
		},
	}
}
