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
// permissions and limitations under the License.

package sqsevent

import (
	"fmt"
	"testing"
	"time"

	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func TestGetTime_Success(t *testing.T) {
	testTimeStr := "2020-07-01T22:19:58Z"
	testTime, err := time.Parse(time.RFC3339, testTimeStr)
	h.Ok(t, err)
	asgLifecycleTime := EventBridgeEvent{Time: testTimeStr}.getTime()
	h.Assert(t, testTime == asgLifecycleTime, "RFC3339 should be parsed correctly from event")
}

func TestGetTime_Empty(t *testing.T) {
	testTimeStr := ""
	testTime := time.Now()
	asgLifecycleTime := EventBridgeEvent{Time: testTimeStr}.getTime()
	h.Assert(t, asgLifecycleTime.After(testTime), "an empty time should return the current time")
}

func TestGetNodeInfo_WithTags(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(
			"i-beebeebe",
			"mydns.example.com",
			map[string]string{
				"name":     "lisa",
				ASGTagName: "test-asg",
			}),
	}
	monitor := SQSMonitor{
		EC2: ec2Mock,
		ASG: h.MockedASG{},
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, "i-0123456789", nodeInfo.InstanceID)
	h.Equals(t, "mydns.example.com", nodeInfo.Name)
	h.Equals(t, "lisa", nodeInfo.Tags["name"])
	h.Equals(t, "test-asg", nodeInfo.Tags[ASGTagName])
	h.Equals(t, true, nodeInfo.IsManaged)
}

func TestGetNodeInfo_BothTags_Managed(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp(
			"i-beebeebe",
			"mydns.example.com",
			map[string]string{
				"aws-nth/managed": "true",
				ASGTagName:        "test-asg",
			}),
	}
	monitor := SQSMonitor{
		EC2:            ec2Mock,
		ASG:            h.MockedASG{},
		CheckIfManaged: true,
		ManagedTag:     "aws-nth/managed",
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, true, nodeInfo.IsManaged)
}

func TestGetNodeInfo_NoASG_Managed(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("i-beebeebe", "mydns.example.com", map[string]string{}),
	}
	monitor := SQSMonitor{
		EC2: ec2Mock,
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, "", nodeInfo.AsgName)
	h.Equals(t, true, nodeInfo.IsManaged)
}

func TestGetNodeInfo_NoASG_NotManaged(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("i-beebeebe", "mydns.example.com", map[string]string{}),
	}
	monitor := SQSMonitor{
		EC2:            ec2Mock,
		CheckIfManaged: true,
		ManagedTag:     "aws-nth/managed",
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, "", nodeInfo.AsgName)
	h.Equals(t, false, nodeInfo.IsManaged)
}

func TestGetNodeInfo_ASG(t *testing.T) {
	asgName := "my-asg"
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("i-beebeebe", "mydns.example.com", map[string]string{}),
	}
	asgMock := h.MockedASG{
		DescribeAutoScalingInstancesResp: autoscaling.DescribeAutoScalingInstancesOutput{
			AutoScalingInstances: []*autoscaling.InstanceDetails{
				{AutoScalingGroupName: &asgName},
			},
		},
	}
	monitor := SQSMonitor{
		EC2: ec2Mock,
		ASG: asgMock,
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	// CheckIfManaged defaults to false; therefore, do not call ASG API
	h.Equals(t, "", nodeInfo.AsgName)
	h.Equals(t, true, nodeInfo.IsManaged)
}

func TestGetNodeInfo_ASG_ASGManaged(t *testing.T) {
	asgName := "test-asg"
	managedTag := "aws-nth/managed"
	tags := map[string]string{managedTag: "", "aws:autoscaling:groupName": asgName}
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("i-beebeebe", "mydns.example.com", tags),
	}
	monitor := SQSMonitor{
		EC2:            ec2Mock,
		CheckIfManaged: true,
		ManagedTag:     managedTag,
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, asgName, nodeInfo.AsgName)
	h.Equals(t, true, nodeInfo.IsManaged)
}

func TestGetNodeInfo_ASG_ASGNotManaged(t *testing.T) {
	asgName := "test-asg"
	tags := map[string]string{"aws:autoscaling:groupName": asgName}
	ec2Mock := h.MockedEC2{
		DescribeInstancesResp: getDescribeInstancesResp("i-beebeebe", "mydns.example.com", tags),
	}
	monitor := SQSMonitor{
		EC2:            ec2Mock,
		CheckIfManaged: true,
		ManagedTag:     "aws-nth/managed",
	}
	nodeInfo, err := monitor.getNodeInfo("i-0123456789")
	h.Ok(t, err)
	h.Equals(t, asgName, nodeInfo.AsgName)
	h.Equals(t, false, nodeInfo.IsManaged)
}

func TestGetNodeInfo_Err(t *testing.T) {
	ec2Mock := h.MockedEC2{
		DescribeInstancesErr: fmt.Errorf("error"),
	}
	monitor := SQSMonitor{
		EC2: ec2Mock,
	}
	_, err := monitor.getNodeInfo("i-0123456789")
	h.Nok(t, err)
}

// AWS Mock helpers specific to sqs-monitor internal tests

func getDescribeInstancesResp(instanceID string, privateDNSName string, tags map[string]string) ec2.DescribeInstancesOutput {
	awsTags := []*ec2.Tag{}

	for k, v := range tags {
		awsTags = append(awsTags, &ec2.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: []*ec2.Instance{
					{
						InstanceId: aws.String(instanceID),
						Placement: &ec2.Placement{
							AvailabilityZone: aws.String("us-east-2a"),
							GroupName:        aws.String(""),
							Tenancy:          aws.String("default"),
						},
						InstanceType:   aws.String("t3.medium"),
						PrivateDnsName: aws.String(privateDNSName),
						Tags:           awsTags,
					},
				},
			},
		},
	}
}
