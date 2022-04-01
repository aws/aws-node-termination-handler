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
	"github.com/aws/aws-node-termination-handler/pkg/logging/sqs"

	knativelogging "knative.dev/pkg/logging"
)

var (
	NewDeleteMessageInputMarshaler  = sqs.NewDeleteMessageInputMarshaler
	NewMessageMarshaler             = sqs.NewMessageMarshaler
	NewReceiveMessageInputMarshaler = sqs.NewReceiveMessageInputMarshaler

	// Alias these so the codebase doesn't have to import two separate packages
	// for logging.
	WithLogger  = knativelogging.WithLogger
	FromContext = knativelogging.FromContext
)
