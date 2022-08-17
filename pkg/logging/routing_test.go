// Copyright 2016-2022 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package logging_test

import (
	"strings"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/logging"
	h "github.com/aws/aws-node-termination-handler/pkg/test"

	"github.com/rs/zerolog"
)

func TestWrite(t *testing.T) {
	buf := &strings.Builder{}
	errBuf := &strings.Builder{}

	r := logging.RoutingLevelWriter{Writer: buf, ErrWriter: errBuf}

	const s = "this is a test"
	p := []byte(s)
	n, err := r.Write(p)

	h.Ok(t, err)
	h.Equals(t, len(p), n)

	h.Equals(t, errBuf.Len(), 0)

	h.Assert(t, buf.Len() > 0, "no message was written to the default location")
	h.Assert(t, strings.Contains(buf.String(), s), "expected message not found in default location")
}

func TestWriteLevel_lessThanWarning(t *testing.T) {
	buf := &strings.Builder{}
	errBuf := &strings.Builder{}

	r := logging.RoutingLevelWriter{Writer: buf, ErrWriter: errBuf}

	const s = "this is a test"
	p := []byte(s)
	n, err := r.WriteLevel(zerolog.InfoLevel, p)

	h.Ok(t, err)
	h.Equals(t, len(p), n)

	h.Equals(t, errBuf.Len(), 0)

	h.Assert(t, buf.Len() > 0, "no message was written to the default location")
	h.Assert(t, strings.Contains(buf.String(), s), "expected message not found in default location")
}

func TestWriteLevel_warning(t *testing.T) {
	buf := &strings.Builder{}
	errBuf := &strings.Builder{}

	r := logging.RoutingLevelWriter{Writer: buf, ErrWriter: errBuf}

	const s = "this is a test"
	p := []byte(s)
	n, err := r.WriteLevel(zerolog.WarnLevel, p)

	h.Ok(t, err)
	h.Equals(t, len(p), n)

	h.Equals(t, buf.Len(), 0)

	h.Assert(t, errBuf.Len() > 0, "no message was written to the error location")
	h.Assert(t, strings.Contains(errBuf.String(), s), "expected message not found in error location")
}

func TestWriteLevel_greaterThanWarning(t *testing.T) {
	buf := &strings.Builder{}
	errBuf := &strings.Builder{}

	r := logging.RoutingLevelWriter{Writer: buf, ErrWriter: errBuf}

	const s = "this is a test"
	p := []byte(s)
	n, err := r.WriteLevel(zerolog.ErrorLevel, p)

	h.Ok(t, err)
	h.Equals(t, len(p), n)

	h.Equals(t, buf.Len(), 0)

	h.Assert(t, errBuf.Len() > 0, "no message was written to the error location")
	h.Assert(t, strings.Contains(errBuf.String(), s), "expected message not found in error location")
}
