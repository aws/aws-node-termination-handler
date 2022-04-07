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

	"github.com/aws/aws-node-termination-handler/pkg/event/asgterminate/lifecycleaction"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type EC2InstanceTerminateLifecycleAction struct {
	ASGLifecycleActionCompleter
	AWSEvent
}

func (e EC2InstanceTerminateLifecycleAction) EC2InstanceIDs() []string {
	return []string{e.Detail.EC2InstanceID}
}

func (e EC2InstanceTerminateLifecycleAction) Done(ctx context.Context) (bool, error) {
	return lifecycleaction.Complete(ctx, e, lifecycleaction.Input{
		AutoScalingGroupName: e.Detail.AutoScalingGroupName,
		LifecycleActionToken: e.Detail.LifecycleActionToken,
		LifecycleHookName:    e.Detail.LifecycleHookName,
		EC2InstanceID:        e.Detail.EC2InstanceID,
	})
}

func (e EC2InstanceTerminateLifecycleAction) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(e.AWSEvent).AddTo(enc)
	return nil
}
