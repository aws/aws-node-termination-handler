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

package v0

import (
	"context"

	"github.com/aws/aws-node-termination-handler/pkg/terminator"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type EC2InstanceRebalanceRecommendation AWSEvent

func (EC2InstanceRebalanceRecommendation) Kind() terminator.EventKind {
	return terminator.EventKinds.RebalanceRecommendation
}

func (e EC2InstanceRebalanceRecommendation) EC2InstanceIDs() []string {
	return []string{e.Detail.InstanceID}
}

func (EC2InstanceRebalanceRecommendation) Done(_ context.Context) (bool, error) {
	return false, nil
}

func (e EC2InstanceRebalanceRecommendation) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(AWSEvent(e)).AddTo(enc)
	return nil
}
