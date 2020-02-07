// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package drainevent

import (
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
)

const (
	// SpotITNKind is a const to define a Spot ITN kind of drainable event
	SpotITNKind = "SPOT_ITN"
)

// MonitorForSpotITNEvents continuously monitors metadata for spot ITNs and sends drain events to the passed in channel
func MonitorForSpotITNEvents(drainChan chan<- DrainEvent, cancelChan chan<- DrainEvent, imds *ec2metadata.EC2MetadataService) error {
	drainEvent, err := checkForSpotInterruptionNotice(imds)
	if err != nil {
		return err
	}
	if drainEvent != nil && drainEvent.Kind == SpotITNKind {
		log.Println("Sending drain event to the drain channel")
		drainChan <- *drainEvent
	}
	return nil
}

// checkForSpotInterruptionNotice Checks EC2 instance metadata for a spot interruption termination notice
func checkForSpotInterruptionNotice(imds *ec2metadata.EC2MetadataService) (*DrainEvent, error) {
	instanceAction, ok, err := imds.GetSpotITNEvent()
	// if there are no spot itns and no errors
	if !ok && err == nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("There was a problem checking for spot ITNs: %w", err)
	}
	interruptionTime, err := time.Parse(time.RFC3339, instanceAction.Time)
	if err != nil {
		return nil, fmt.Errorf("Could not parse time from spot interruption notice metadata json: %w", err)
	}
	return &DrainEvent{
		EventID:     instanceAction.Id,
		Kind:        SpotITNKind,
		StartTime:   interruptionTime,
		Description: fmt.Sprintf("Spot ITN received. %s will be %s at %s \n", instanceAction.Detail.InstanceId, instanceAction.Detail.InstanceAction, instanceAction.Time),
	}, nil
}
