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

package interruptioneventstore

import (
	"sync"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent"
)

// Store is the drain event store data structure
type Store struct {
	sync.RWMutex
	NthConfig              config.Config
	interruptionEventStore map[string]*interruptionevent.InterruptionEvent
	ignoredEvents          map[string]struct{}
	atLeastOneEvent        bool
}

// New Creates a new interruption event store
func New(nthConfig config.Config) *Store {
	return &Store{
		NthConfig:              nthConfig,
		interruptionEventStore: make(map[string]*interruptionevent.InterruptionEvent),
		ignoredEvents:          make(map[string]struct{}),
	}
}

// CancelInterruptionEvent removes an interruption event from the internal store
func (s *Store) CancelInterruptionEvent(eventID string) {
	s.Lock()
	defer s.Unlock()
	delete(s.interruptionEventStore, eventID)
}

// AddInterruptionEvent adds an interruption event to the internal store
func (s *Store) AddInterruptionEvent(interruptionEvent *interruptionevent.InterruptionEvent) {
	s.RLock()
	_, ok := s.interruptionEventStore[interruptionEvent.EventID]
	s.RUnlock()
	if ok {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.interruptionEventStore[interruptionEvent.EventID] = interruptionEvent
	if _, ignored := s.ignoredEvents[interruptionEvent.EventID]; !ignored {
		s.atLeastOneEvent = true
	}
	return
}

// GetActiveEvent returns true if there are interruption events in the internal store
func (s *Store) GetActiveEvent() (*interruptionevent.InterruptionEvent, bool) {
	s.RLock()
	defer s.RUnlock()
	for _, interruptionEvent := range s.interruptionEventStore {
		if s.shouldEventDrain(interruptionEvent) {
			return interruptionEvent, true
		}
	}
	return &interruptionevent.InterruptionEvent{}, false
}

// ShouldDrainNode returns true if there are drainable events in the internal store
func (s *Store) ShouldDrainNode() bool {
	s.RLock()
	defer s.RUnlock()
	for _, interruptionEvent := range s.interruptionEventStore {
		if s.shouldEventDrain(interruptionEvent) {
			return true
		}
	}
	return false
}

func (s *Store) shouldEventDrain(interruptionEvent *interruptionevent.InterruptionEvent) bool {
	_, ignored := s.ignoredEvents[interruptionEvent.EventID]
	if !ignored && !interruptionEvent.Drained && s.TimeUntilDrain(interruptionEvent) <= 0 {
		return true
	}
	return false
}

// TimeUntilDrain returns the duration until a node drain should occur (can return a negative duration)
func (s *Store) TimeUntilDrain(interruptionEvent *interruptionevent.InterruptionEvent) time.Duration {
	nodeTerminationGracePeriod := time.Duration(s.NthConfig.NodeTerminationGracePeriod) * time.Second
	drainTime := interruptionEvent.StartTime.Add(-1 * nodeTerminationGracePeriod)
	return drainTime.Sub(time.Now())
}

// MarkAllAsDrained should be called after the node has been drained to prevent further unnecessary drain calls to the k8s api
func (s *Store) MarkAllAsDrained(nodeName string) {
	s.Lock()
	defer s.Unlock()
	for _, interruptionEvent := range s.interruptionEventStore {
		if interruptionEvent.NodeName == nodeName {
			interruptionEvent.Drained = true
		}
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

// ShouldUncordonNode returns true if there was a interruption event but it was canceled and the store is now empty or only consists of ignored events
func (s *Store) ShouldUncordonNode(nodeName string) bool {
	s.RLock()
	defer s.RUnlock()
	if !s.atLeastOneEvent {
		return false
	}
	if len(s.interruptionEventStore) == 0 {
		return true
	}

	for _, interruptionEvent := range s.interruptionEventStore {
		if _, ignored := s.ignoredEvents[interruptionEvent.EventID]; !ignored && interruptionEvent.NodeName == nodeName {
			return false
		}
	}

	return true
}
