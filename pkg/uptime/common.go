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
	"fmt"
	"os"
	"strconv"
	"strings"
)

// UptimeFuncType cleans up function arguments or return type.
type UptimeFuncType func() (int64, error)

// UptimeFromFile reads system uptime information from filepath and returns
// the number of seconds since last system boot.
func UptimeFromFile(filepath string) (int64, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return 0, fmt.Errorf("Not able to read %s: %w", filepath, err)
	}

	uptime, err := strconv.ParseFloat(strings.Split(string(data), " ")[0], 64)
	if err != nil {
		return 0, fmt.Errorf("Not able to parse %s to int64: %w", filepath, err)
	}
	return int64(uptime), nil
}
