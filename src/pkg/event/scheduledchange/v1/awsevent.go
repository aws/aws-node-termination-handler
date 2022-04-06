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
	"github.com/aws/aws-node-termination-handler/pkg/event"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AWSEvent contains the properties defined in AWS EventBridge schema
// aws.health@AWSHealthEvent v1.
type AWSEvent struct {
	event.AWSMetadata

	Detail AWSHealthEventDetail `json:"detail"`
}

func (e AWSEvent) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	zap.Inline(e.AWSMetadata).AddTo(enc)
	enc.AddObject("detail", e.Detail)
	return nil
}

type AWSHealthEventDetail struct {
	EventARN          string             `json:"eventArn"`
	EventTypeCode     string             `json:"eventTypeCode"`
	Service           string             `json:"service"`
	EventDescription  []EventDescription `json:"eventDescription"`
	StartTime         string             `json:"startTime"`
	EndTime           string             `json:"endTime"`
	EventTypeCategory string             `json:"eventTypeCategory"`
	AffectedEntities  []AffectedEntity   `json:"affectedEntities"`
}

func (e AWSHealthEventDetail) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("eventArn", e.EventARN)
	enc.AddString("eventTypeCode", e.EventTypeCode)
	enc.AddString("eventTypeCategory", e.EventTypeCategory)
	enc.AddString("service", e.Service)
	enc.AddString("startTime", e.StartTime)
	enc.AddString("endTime", e.EndTime)
	enc.AddArray("eventDescription", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, desc := range e.EventDescription {
			enc.AppendObject(desc)
		}
		return nil
	}))
	enc.AddArray("affectedEntities", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, entity := range e.AffectedEntities {
			enc.AppendObject(entity)
		}
		return nil
	}))
	return nil
}

type EventDescription struct {
	LatestDescription string `json:"latestDescription"`
	Language          string `json:"language"`
}

func (e EventDescription) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("latestDescription", e.LatestDescription)
	enc.AddString("language", e.Language)
	return nil
}

type AffectedEntity struct {
	EntityValue string `json:"entityValue"`
}

func (e AffectedEntity) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("entityValue", e.EntityValue)
	return nil
}
