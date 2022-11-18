// Copyright 2016-2022 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package logging

import (
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/rs/zerolog/log"
)

type versionedMsgsV1 struct{}

func (versionedMsgsV1) MonitoringStarted(monitorKind string) {
	log.Info().Str("event_type", monitorKind).Msg("Started monitoring for events")
}

func (versionedMsgsV1) ProblemMonitoringForEvents(monitorKind string, err error) {
	log.Warn().Str("event_type", monitorKind).Err(err).Msg("There was a problem monitoring for events")
}

func (versionedMsgsV1) RequestingInstanceDrain(event *monitor.InterruptionEvent) {
	log.Info().
		Str("event-id", event.EventID).
		Str("kind", event.Kind).
		Str("node-name", event.NodeName).
		Str("instance-id", event.InstanceID).
		Str("provider-id", event.ProviderID).
		Msg("Requesting instance drain")
}

func (versionedMsgsV1) SendingInterruptionEventToChannel(_ string) {
	log.Debug().Msg("Sending SQS_TERMINATE interruption event to the interruption channel")
}

type versionedMsgsV2 struct{}

func (versionedMsgsV2) MonitoringStarted(monitorKind string) {
	log.Info().Str("monitor_type", monitorKind).Msg("Started monitoring for events")
}

func (versionedMsgsV2) ProblemMonitoringForEvents(monitorKind string, err error) {
	log.Warn().Str("monitor_type", monitorKind).Err(err).Msg("There was a problem monitoring for events")
}

func (versionedMsgsV2) RequestingInstanceDrain(event *monitor.InterruptionEvent) {
	log.Info().
		Str("event-id", event.EventID).
		Str("kind", event.Kind).
		Str("monitor", event.Monitor).
		Str("node-name", event.NodeName).
		Str("instance-id", event.InstanceID).
		Str("provider-id", event.ProviderID).
		Msg("Requesting instance drain")
}

func (versionedMsgsV2) SendingInterruptionEventToChannel(eventKind string) {
	log.Debug().Msgf("Sending %s interruption event to the interruption channel", eventKind)
}

var VersionedMsgs interface {
	MonitoringStarted(monitorKind string)
	ProblemMonitoringForEvents(monitorKind string, err error)
	RequestingInstanceDrain(event *monitor.InterruptionEvent)
	SendingInterruptionEventToChannel(eventKind string)
} = versionedMsgsV1{}

func SetFormatVersion(version int) error {
	switch version {
	case 1:
		VersionedMsgs = versionedMsgsV1{}
		return nil
	case 2:
		VersionedMsgs = versionedMsgsV2{}
		return nil
	default:
		VersionedMsgs = versionedMsgsV1{}
		return fmt.Errorf("Unrecognized log format version: %d, using version 1", version)
	}
}
