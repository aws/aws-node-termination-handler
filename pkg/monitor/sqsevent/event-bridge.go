// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package sqsevent

import (
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"
)

// EventBridgeEvent is a structure to hold generic event details from Amazon EventBridge
type EventBridgeEvent struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Account    string          `json:"account"`
	Time       string          `json:"time"`
	Region     string          `json:"region"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

func (e EventBridgeEvent) getTime() time.Time {
	terminationTime, err := time.Parse(time.RFC3339, e.Time)
	if err != nil {
		log.Warn().Msgf("Unable to parse time as RFC3339 from event %s (%s), using current time instead.", e.DetailType, e.ID)
		return time.Now()
	}
	return terminationTime
}
