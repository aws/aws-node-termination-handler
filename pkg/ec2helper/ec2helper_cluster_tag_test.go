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

package ec2helper_test

import (
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/ec2helper"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type filterRecordingEC2 struct {
	ec2iface.EC2API
	filters []*ec2.Filter
}

func (m *filterRecordingEC2) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	m.filters = input.Filters
	resp := getDescribeInstancesResp()
	return &resp, nil
}

func filterMap(filters []*ec2.Filter) map[string]string {
	out := map[string]string{}
	for _, f := range filters {
		out[*f.Name] = *f.Values[0]
	}
	return out
}

func TestGetInstanceIdsByTagKeyNoClusterFilter(t *testing.T) {
	ec2Mock := &filterRecordingEC2{}
	_, err := ec2helper.New(ec2Mock).GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)
	h.Equals(t, map[string]string{"tag-key": "myNTHManagedTag"}, filterMap(ec2Mock.filters))
}

func TestGetInstanceIdsByTagKeyWithClusterTag(t *testing.T) {
	ec2Mock := &filterRecordingEC2{}
	_, err := ec2helper.NewWithClusterTag(ec2Mock, "example.com/cluster", "my-cluster").GetInstanceIdsByTagKey("myNTHManagedTag")
	h.Ok(t, err)
	h.Equals(t, map[string]string{"tag-key": "myNTHManagedTag", "tag:example.com/cluster": "my-cluster"}, filterMap(ec2Mock.filters))
}
