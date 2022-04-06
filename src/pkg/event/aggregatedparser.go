/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

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

package event

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
)

type (
	Parser interface {
		Parse(context.Context, string) terminator.Event
	}

	AggregatedParser []Parser
)

func NewAggregatedParser(parsers ...Parser) AggregatedParser {
	return AggregatedParser(parsers)
}

func (p AggregatedParser) Parse(ctx context.Context, str string) terminator.Event {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("event.parser"))

	if str == "" {
		return noop{}
	}

	for _, parser := range p {
		if a := parser.Parse(ctx, str); a != nil {
			return a
		}
	}

	md := AWSMetadata{}
	if err := json.Unmarshal([]byte(str), &md); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to parse SQS message metadata")
		return noop{}
	}
	return noop(md)
}
