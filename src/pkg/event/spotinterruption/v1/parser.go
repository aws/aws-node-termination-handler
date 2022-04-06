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

package v1

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
)

const (
	source     = "aws.ec2"
	detailType = "EC2 Spot Instance Interruption Warning"
	version    = "1"
)

type Parser struct{}

func (Parser) Parse(ctx context.Context, str string) terminator.Event {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("spotInterruption.v1"))

	evt := EC2SpotInstanceInterruptionWarning{}
	if err := json.Unmarshal([]byte(str), &evt); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to unmarshal EC2 spot instance interruption event")
		return nil
	}

	if evt.Source != source || evt.DetailType != detailType || evt.Version != version {
		return nil
	}

	return evt
}
