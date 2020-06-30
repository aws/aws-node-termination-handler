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
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	getTickCount = kernel32.NewProc("GetTickCount")
)

// Uptime returns the number of seconds since last system boot.
func Uptime() (int64, error) {
	millis, _, err := syscall.Syscall(getTickCount.Addr(), 0, 0, 0, 0)
	if err != 0 {
		return 0, err
	}
	uptime := (time.Duration(millis) * time.Millisecond).Seconds()
	return int64(uptime), nil
}
