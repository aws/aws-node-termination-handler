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

package v0

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-node-termination-handler/pkg/event"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
)

const (
	source     = "aws.ec2"
	detailType = "EC2 Instance Rebalance Recommendation"
	version    = "0"
)

func NewParser() event.Parser {
	return event.ParserFunc(parse)
}

func parse(ctx context.Context, str string) event.Event {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("rebalanceRecommendation.v0"))

	evt := Ec2InstanceRebalanceRecommendation{}
	if err := json.Unmarshal([]byte(str), &evt); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to unmarshal EC2 instance rebalance recommendation event")
		return nil
	}

	if evt.Source != source || evt.DetailType != detailType || evt.Version != version {
		return nil
	}

	return evt
}
