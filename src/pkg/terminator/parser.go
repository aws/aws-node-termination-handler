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

package terminator

import (
	"context"
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/event"

	"github.com/aws/aws-sdk-go/service/sqs"
)

type eventParserAdapter struct {
	event.Parser
}

func NewSQSMessageParser(parser event.Parser) (SQSMessageParser, error) {
	if parser == nil {
		return nil, fmt.Errorf("argument 'parser' is nil")
	}
	return eventParserAdapter{Parser: parser}, nil
}

func (a eventParserAdapter) Parse(ctx context.Context, msg *sqs.Message) event.Event {
	if msg == nil || msg.Body == nil {
		return a.Parser.Parse(ctx, "")
	}
	return a.Parser.Parse(ctx, *msg.Body)
}
