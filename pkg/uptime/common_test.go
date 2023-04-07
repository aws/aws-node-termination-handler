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

package uptime

import (
	"os"
	"testing"

	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

const testFile = "test.out"

func TestUptimeFromFileSuccess(t *testing.T) {
	d1 := []byte("350735.47 234388.90")
	err := os.WriteFile(testFile, d1, 0644)
	h.Ok(t, err)

	value, err := UptimeFromFile(testFile)
	os.Remove(testFile)
	h.Ok(t, err)
	h.Equals(t, int64(350735), value)
}

func TestUptimeFromFileReadFail(t *testing.T) {
	_, err := UptimeFromFile("does-not-exist")
	h.Assert(t, err != nil, "Failed to return error when ReadFile failed")
}

func TestUptimeFromFileBadData(t *testing.T) {
	d1 := []byte("Something not time")
	err := os.WriteFile(testFile, d1, 0644)
	h.Ok(t, err)

	_, err = UptimeFromFile(testFile)
	os.Remove(testFile)
	h.Assert(t, err != nil, "Failed to return error for int64 parse")
}
