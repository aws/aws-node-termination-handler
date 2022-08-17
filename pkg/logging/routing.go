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

package logging

import (
	"io"

	"github.com/rs/zerolog"
)

// RoutingLevelWriter writes data to one of two locations based on an
// associated level value.
type RoutingLevelWriter struct {
	io.Writer
	ErrWriter io.Writer
}

// WriteLevel if *l* is warning or higher then *b* is written to the error
// location, otherwise it is written to the default location.
func (r RoutingLevelWriter) WriteLevel(l zerolog.Level, b []byte) (int, error) {
	if l < zerolog.WarnLevel {
		return r.Write(b)
	}
	return r.ErrWriter.Write(b)
}
