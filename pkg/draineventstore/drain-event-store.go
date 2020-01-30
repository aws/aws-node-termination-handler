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
// permissions and limitations under the License

package draineventstore

import (
	"sync"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
)

// Store is a the drain event store data structure
type Store struct {
	sync.RWMutex
	NthConfig       config.Config
	drainEventStore map[string]*drainevent.DrainEvent
	ignoredEvents   map[string]struct{}
	atLeastOneEvent bool
}

// New Creates a new drain event store
func New(nthConfig config.Config) *Store {
	return &Store{
		NthConfig:       nthConfig,
		drainEventStore: make(map[string]*drainevent.DrainEvent),
		ignoredEvents:   make(map[string]struct{}),
	}
}

// CancelDrainEvent removes a drain event from the internal store
func (s *Store) CancelDrainEvent(eventID string) {
	s.Lock()
	defer s.Unlock()
	delete(s.drainEventStore, eventID)
}

// AddDrainEvent adds a drain event to the internal store
func (s *Store) AddDrainEvent(drainEvent *drainevent.DrainEvent) {
	s.RLock()
	_, ok := s.drainEventStore[drainEvent.EventID]
	s.RUnlock()
	if ok {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.drainEventStore[drainEvent.EventID] = drainEvent
	if _, ignored := s.ignoredEvents[drainEvent.EventID]; !ignored {
		s.atLeastOneEvent = true
	}
	return
}

// GetActiveEvent returns true if there are drainable events in the internal store
func (s *Store) GetActiveEvent() (*drainevent.DrainEvent, bool) {
	s.RLock()
	defer s.RUnlock()
	for _, drainEvent := range s.drainEventStore {
		if s.shouldEventDrain(drainEvent) {
			return drainEvent, true
		}
	}
	return &drainevent.DrainEvent{}, false
}

// ShouldDrainNode returns true if there are drainable events in the internal store
func (s *Store) ShouldDrainNode() bool {
	s.RLock()
	defer s.RUnlock()
	for _, drainEvent := range s.drainEventStore {
		if s.shouldEventDrain(drainEvent) {
			return true
		}
	}
	return false
}

func (s *Store) shouldEventDrain(drainEvent *drainevent.DrainEvent) bool {
	_, ignored := s.ignoredEvents[drainEvent.EventID]
	if !ignored && !drainEvent.Drained && s.TimeUntilDrain(drainEvent) <= 0 {
		return true
	}
	return false
}

// TimeUntilDrain returns the duration until a node drain should occur (can return a negative duration)
func (s *Store) TimeUntilDrain(drainEvent *drainevent.DrainEvent) time.Duration {
	nodeTerminationGracePeriod := time.Duration(s.NthConfig.NodeTerminationGracePeriod) * time.Second
	drainTime := drainEvent.StartTime.Add(-1 * nodeTerminationGracePeriod)
	return drainTime.Sub(time.Now())
}

// MarkAllAsDrained should be called after the node has been drained to prevent further unnecessary drain calls to the k8s api
func (s *Store) MarkAllAsDrained() {
	s.Lock()
	defer s.Unlock()
	for _, drainEvent := range s.drainEventStore {
		drainEvent.Drained = true
	}
}

// IgnoreEvent will store an event ID so that monitor loops cannot write to the store with the same event ID
// Drain actions are ignored on the passed in event ID by setting the Drained flag to true
func (s *Store) IgnoreEvent(eventID string) {
	if eventID == "" {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.ignoredEvents[eventID] = struct{}{}
}

// ShouldUncordonNode returns true if there was a drainable event but it was cancelled and the store is now empty or only consists of ignored events
func (s *Store) ShouldUncordonNode() bool {
	s.RLock()
	defer s.RUnlock()
	if !s.atLeastOneEvent {
		return false
	}
	if len(s.drainEventStore) == 0 {
		return true
	}
	for _, drainEvent := range s.drainEventStore {
		if _, ignored := s.ignoredEvents[drainEvent.EventID]; !ignored {
			return false
		}
	}
	return true
}
