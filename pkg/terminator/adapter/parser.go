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

package adapter

import (
	"context"

	"github.com/aws/aws-node-termination-handler/pkg/event"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"

	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type EventParser struct {
	event.Parser
}

func (e EventParser) Parse(ctx context.Context, msg sqstypes.Message) terminator.Event {
	if msg.Body == nil {
		return e.Parser.Parse(ctx, "")
	}
	return e.Parser.Parse(ctx, *msg.Body)
}
