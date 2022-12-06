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

package config

import (
	"flag"
	"strconv"
	"testing"

	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

// All of these needed for TestIsConfigProvided
var location string
var cliArgName = "name"
var envVarName = "NAME_TEST"
var value = "haugenj"

func init() {
	flag.StringVar(&location, cliArgName, value, value)
}

func TestGetEnv(t *testing.T) {
	var key = "STRING_TEST"
	var successVal = "success"
	var failVal = "failure"

	t.Setenv(key, successVal)

	result := getEnv(key+"bla", failVal)
	h.Equals(t, failVal, result)

	result = getEnv(key, failVal)
	h.Equals(t, successVal, result)
}

func TestGetIntEnv(t *testing.T) {
	var key = "INT_TEST"
	var successVal = 1
	var failVal = 0

	t.Setenv(key, strconv.Itoa(successVal))

	result := getIntEnv(key+"bla", failVal)
	h.Equals(t, failVal, result)

	result = getIntEnv(key, failVal)
	h.Equals(t, successVal, result)

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("getIntEnv did not panic")
		}
	}()
	t.Setenv(key, "hi")
	getIntEnv(key, 0)
}

func TestGetBoolEnv(t *testing.T) {
	var key = "BOOL_TEST"
	var successVal = true
	var failVal = false

	t.Setenv(key, strconv.FormatBool(successVal))

	result := getBoolEnv(key+"bla", failVal)
	h.Equals(t, failVal, result)

	result = getBoolEnv(key, failVal)
	h.Equals(t, successVal, result)

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("getBoolEnv did not panic")
		}
	}()
	t.Setenv(key, "hi")
	getBoolEnv(key, false)
}

func TestIsConfigProvided(t *testing.T) {
	result := isConfigProvided(cliArgName, envVarName)
	h.Equals(t, false, result)

	err := flag.Set(cliArgName, value)
	h.Ok(t, err)
	result = isConfigProvided(cliArgName, envVarName)
	h.Equals(t, true, result)

	t.Setenv(envVarName, value)
	result = isConfigProvided(cliArgName, envVarName)
	h.Equals(t, true, result)
}
