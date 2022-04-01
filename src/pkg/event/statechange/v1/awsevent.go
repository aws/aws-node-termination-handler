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
	"github.com/aws/aws-node-termination-handler/pkg/event"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AwsEvent contains the properties defined in AWS EventBridge schema
// aws.ec2@EC2InstanceStateChangeNotification v1.
type AwsEvent struct {
	event.AwsMetadata

	Detail Ec2InstanceStateChangeNotificationDetail `json:"detail"`
}

func (e AwsEvent) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(e.AwsMetadata).AddTo(enc)
	enc.AddObject("detail", e.Detail)
	return nil
}

type Ec2InstanceStateChangeNotificationDetail struct {
	InstanceId string `json:"instance-id"`
	State      string `json:"state"`
}

func (e Ec2InstanceStateChangeNotificationDetail) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("instance-id", e.InstanceId)
	enc.AddString("state", e.State)
	return nil
}
