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
	"os"
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

func TestParseCliArgsUnmarshalFailure(t *testing.T) {
	var saveFlags = make(map[string]flagData)
	for k, v := range flags {
		saveFlags[k] = v
	}

	flags["delete-local-data"] = flagData{
		Key:      deleteLocalDataConfigKey,
		DefValue: 123,
		Usage:    "If true, do not drain pods that are using local node storage in emptyDir",
	}
	_, err := ParseCliArgs()
	h.Assert(t, true, "Failed to return error when unmarshal failed", err != nil)

	flags = saveFlags
}

func TestCreateFlags(t *testing.T) {
	var key = "KEY"

	var validStringValue = map[string]flagData{
		"test-string": flagData{
			Key:      key,
			DefValue: "default",
			Usage:    "description",
		},
	}
	result, err := createFlags(validStringValue)
	h.Ok(t, err)
	h.Equals(t, "default", result["test-string"])

	var validIntValue = map[string]flagData{
		"test-int": flagData{
			Key:      key,
			DefValue: 1234,
			Usage:    "description",
		},
	}
	result, err = createFlags(validIntValue)
	h.Ok(t, err)
	h.Equals(t, 1234, result["test-int"])

	var validBoolValue = map[string]flagData{
		"test-bool": flagData{
			Key:      key,
			DefValue: false,
			Usage:    "description",
		},
	}
	result, err = createFlags(validBoolValue)
	h.Ok(t, err)
	h.Equals(t, false, result["test-bool"])

	os.Setenv(key, "bla")
	var invalidDefValue = map[string]flagData{
		"test-string": flagData{
			Key:      key,
			DefValue: 7.9,
			Usage:    "description",
		},
	}
	_, err = createFlags(invalidDefValue)
	h.Assert(t, true, "Failed to return error when defValue type unrecognized", err != nil)

	var invalidIntEnvValue = map[string]flagData{
		"test-string": flagData{
			Key:      key,
			DefValue: 1,
			Usage:    "description",
		},
	}
	_, err = createFlags(invalidIntEnvValue)
	h.Assert(t, true, "Failed to return error when env var not integer", err != nil)

	var invalidBoolEnvValue = map[string]flagData{
		"test-string": flagData{
			Key:      key,
			DefValue: false,
			Usage:    "description",
		},
	}
	_, err = createFlags(invalidBoolEnvValue)
	h.Assert(t, true, "Failed to return error when env var not boolean", err != nil)
}

func TestGetEnv(t *testing.T) {
	var key = "STRING_TEST"
	var successVal = "success"
	var failVal = "failure"

	os.Setenv(key, successVal)

	result := getEnv(key+"bla", failVal)
	h.Equals(t, failVal, result)

	result = getEnv(key, failVal)
	h.Equals(t, successVal, result)
}

func TestGetIntEnv(t *testing.T) {
	var key = "INT_TEST"
	var successVal = 1
	var failVal = 0

	os.Setenv(key, strconv.Itoa(successVal))

	result, ok := getIntEnv(key+"bla", failVal)
	h.Ok(t, ok)
	h.Equals(t, failVal, result)

	result, ok = getIntEnv(key, failVal)
	h.Ok(t, ok)
	h.Equals(t, successVal, result)

	os.Setenv(key, "bla")
	result, ok = getIntEnv(key, failVal)
	h.Assert(t, true, "Failed to return error when var not integer.", ok != nil)
}

func TestGetBoolEnv(t *testing.T) {
	var key = "BOOL_TEST"
	var successVal = true
	var failVal = false

	os.Setenv(key, strconv.FormatBool(successVal))

	result, err := getBoolEnv(key+"bla", failVal)
	h.Ok(t, err)
	h.Equals(t, failVal, result)

	result, err = getBoolEnv(key, failVal)
	h.Ok(t, err)
	h.Equals(t, successVal, result)

	os.Setenv(key, "bla")
	result, err = getBoolEnv(key, failVal)
	h.Assert(t, true, "Failed to return error when var not boolean.", err != nil)
}

func TestIsConfigProvided(t *testing.T) {
	result := isConfigProvided(cliArgName, envVarName)
	h.Equals(t, false, result)

	flag.Set(cliArgName, value)
	result = isConfigProvided(cliArgName, envVarName)
	h.Equals(t, true, result)

	os.Setenv(envVarName, value)
	result = isConfigProvided(cliArgName, envVarName)
	h.Equals(t, true, result)
}
