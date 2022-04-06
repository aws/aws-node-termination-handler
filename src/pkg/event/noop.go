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

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type noop AWSMetadata

func (n noop) EC2InstanceIDs() []string {
	return []string{}
}

func (n noop) Done(_ context.Context) (bool, error) {
	return false, nil
}

func (n noop) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(AWSMetadata(n)).AddTo(enc)
	return nil
}
