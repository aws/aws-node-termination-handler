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

package v1

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Ec2InstanceTerminateLifecycleAction struct {
	AsgLifecycleActionCompleter
	AwsEvent
}

func (e Ec2InstanceTerminateLifecycleAction) Ec2InstanceIds() []string {
	return []string{e.Detail.Ec2InstanceId}
}

func (e Ec2InstanceTerminateLifecycleAction) Done(ctx context.Context) (bool, error) {
	if _, err := e.CompleteLifecycleActionWithContext(ctx, &autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  aws.String(e.Detail.AutoScalingGroupName),
		LifecycleActionResult: aws.String("CONTINUE"),
		LifecycleHookName:     aws.String(e.Detail.LifecycleHookName),
		LifecycleActionToken:  aws.String(e.Detail.LifecycleActionToken),
		InstanceId:            aws.String(e.Detail.Ec2InstanceId),
	}); err != nil {
		var f awserr.RequestFailure
		return errors.As(err, &f) && f.StatusCode() != 400, err
	}

	return false, nil
}

func (e Ec2InstanceTerminateLifecycleAction) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(e.AwsEvent).AddTo(enc)
	return nil
}
