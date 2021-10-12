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

func TestIsASGManaged(t *testing.T) {
	asgName := "test-asg"
	asgMock := h.MockedASG{
		// DescribeAutoScalingInstancesResp: autoscaling.DescribeAutoScalingInstancesOutput{
		// 	AutoScalingInstances: []*autoscaling.InstanceDetails{
		// 		{AutoScalingGroupName: &asgName},
		// 	},
		// },
		DescribeTagsPagesResp: autoscaling.DescribeTagsOutput{
			Tags: []*autoscaling.TagDescription{
				{Key: aws.String("aws-node-termination-handler/managed")},
			},
		},
	}
	monitor := SQSMonitor{
		ASG:            asgMock,
		CheckIfManaged: true,
		ManagedAsgTag:  "aws-node-termination-handler/managed",
	}
	isManaged, err := monitor.isASGManaged(asgName, "i-0123456789")
	h.Ok(t, err)
	h.Equals(t, true, isManaged)
}

// func TestIsInstanceManaged_NotInASG(t *testing.T) {
// 	asgMock := h.MockedASG{
// 		DescribeAutoScalingInstancesResp: autoscaling.DescribeAutoScalingInstancesOutput{
// 			AutoScalingInstances: []*autoscaling.InstanceDetails{},
// 		},
// 	}
// 	monitor := SQSMonitor{ASG: asgMock}
// 	isManaged, err := monitor.isInstanceManaged("i-0123456789")
// 	h.Ok(t, err)
// 	h.Equals(t, false, isManaged)
// }

func TestIsASGManaged_ASGNotManaged(t *testing.T) {
	asgName := "test-asg"
	asgMock := h.MockedASG{
		// DescribeAutoScalingInstancesResp: autoscaling.DescribeAutoScalingInstancesOutput{
		// 	AutoScalingInstances: []*autoscaling.InstanceDetails{
		// 		{AutoScalingGroupName: &asgName},
		// 	},
		// },
		DescribeTagsPagesResp: autoscaling.DescribeTagsOutput{
			Tags: []*autoscaling.TagDescription{},
		},
	}
	monitor := SQSMonitor{ASG: asgMock}
	isManaged, err := monitor.isASGManaged(asgName, "i-0123456789")
	h.Ok(t, err)
	h.Equals(t, false, isManaged)
}

// func TestIsInstanceManaged_Err(t *testing.T) {
// 	asgMock := h.MockedASG{
// 		DescribeAutoScalingInstancesErr: fmt.Errorf("error"),
// 	}
// 	monitor := SQSMonitor{ASG: asgMock}
// 	_, err := monitor.isInstanceManaged("i-0123456789")
// 	h.Nok(t, err)
// }

func TestIsASGManaged_TagErr(t *testing.T) {
	asgName := "test-asg"
	asgMock := h.MockedASG{
		// DescribeAutoScalingInstancesResp: autoscaling.DescribeAutoScalingInstancesOutput{
		// 	AutoScalingInstances: []*autoscaling.InstanceDetails{
		// 		{AutoScalingGroupName: &asgName},
		// 	},
		// },
		DescribeTagsPagesErr: fmt.Errorf("error"),
	}
	monitor := SQSMonitor{ASG: asgMock}
	_, err := monitor.isASGManaged(asgName, "i-0123456789")
	h.Nok(t, err)
}

func TestIsASGManaged_EmptyASGNameErr(t *testing.T) {
	monitor := SQSMonitor{}
	_, err := monitor.isASGManaged("", "i-0123456789")
	h.Nok(t, err)
}
