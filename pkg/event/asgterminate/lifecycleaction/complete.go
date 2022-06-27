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

package lifecycleaction

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

type (
	ASGLifecycleActionCompleter interface {
		CompleteLifecycleActionWithContext(aws.Context, *autoscaling.CompleteLifecycleActionInput, ...request.Option) (*autoscaling.CompleteLifecycleActionOutput, error)
	}

	Input struct {
		AutoScalingGroupName string
		LifecycleActionToken string
		LifecycleHookName    string
		EC2InstanceID        string
	}
)

func Complete(ctx context.Context, completer ASGLifecycleActionCompleter, input Input) (bool, error) {
	if _, err := completer.CompleteLifecycleActionWithContext(ctx, &autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  aws.String(input.AutoScalingGroupName),
		LifecycleActionResult: aws.String("CONTINUE"),
		LifecycleHookName:     aws.String(input.LifecycleHookName),
		LifecycleActionToken:  aws.String(input.LifecycleActionToken),
		InstanceId:            aws.String(input.EC2InstanceID),
	}); err != nil {
		var f awserr.RequestFailure
		return errors.As(err, &f) && f.StatusCode() != 400, err
	}

	return false, nil
}
