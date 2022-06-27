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

package webhook

import (
	"context"
	"time"
)

type (
	sendFuncType func(context.Context, Notification) error

	Event interface {
		EventID() string
		Kind() string
		StartTime() time.Time
	}

	Request struct {
		sendFunc sendFuncType

		Event      Event
		InstanceID string
		NodeName   string
	}
)

func (r Request) Send(ctx context.Context) error {
	return r.sendFunc(ctx, Notification{
		EventID:    r.Event.EventID(),
		InstanceID: r.InstanceID,
		Kind:       r.Event.Kind(),
		NodeName:   r.NodeName,
		StartTime:  r.Event.StartTime(),
	})
}
