/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logging

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// Writer adapts a logger to an `io.Writer`.
type Writer struct {
	*zap.SugaredLogger
}

// Write converts `buf` to a string and sends it to the underlying logger.
// If the string beings with "warn" or "error" (case in-sensitive) the message
// will be logged at the corresponding level; otherwise the level will be
// "info".
func (w Writer) Write(buf []byte) (int, error) {
	if w.SugaredLogger == nil {
		return 0, fmt.Errorf("Writer's backing logger is nil")
	}

	msg := string(buf)
	m := strings.ToLower(msg)
	switch {
	case strings.HasPrefix(m, "error"):
		w.Error(msg)
	case strings.HasPrefix(m, "warn"):
		w.Warn(msg)
	default:
		w.Info(msg)
	}

	return len(buf), nil
}
