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

package node

import (
	"io/ioutil"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"k8s.io/kubectl/pkg/drain"
)

func getNthConfig(t *testing.T) config.Config {
	nthConfig, err := config.ParseCliArgs()
	if err != nil {
		t.Error("failed to create nthConfig")
	}
	return nthConfig
}

func getNode(t *testing.T, drainHelper *drain.Helper) *Node {
	tNode, err := NewWithValues(getNthConfig(t), drainHelper)
	if err != nil {
		t.Error("failed to create node")
	}
	return tNode
}

func TestGetUptimeSuccess(t *testing.T) {
	d1 := []byte("350735.47 234388.90")
	err := ioutil.WriteFile("test.txt", d1, 0644)

	value, err := getSystemUptime("test.txt")
	h.Ok(t, err)
	h.Equals(t, 350735.47, value)
}

func TestGetUptimeFailure(t *testing.T) {
	d1 := []byte("Something not time")
	err := ioutil.WriteFile("test.out", d1, 0644)

	_, err = getSystemUptime("test.out")
	h.Assert(t, err != nil, "Failed to throw error for float64 parse")
}
