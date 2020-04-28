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

package interruptioneventstore_test

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func TestAddDrainEvent(t *testing.T) {
	store := interruptioneventstore.New(config.Config{})

	event1 := &interruptionevent.InterruptionEvent{
		EventID:   "123",
		State:     "Active",
		StartTime: time.Now(),
	}
	store.AddInterruptionEvent(event1)

	storedEvent, isActive := store.GetActiveEvent()
	h.Equals(t, true, isActive)
	h.Equals(t, event1.EventID, storedEvent.EventID)

	// Attempt to add new event with the same eventID
	event2 := &interruptionevent.InterruptionEvent{
		EventID:   "123",
		State:     "Something Else",
		StartTime: time.Now(),
	}

	store.AddInterruptionEvent(event2)
	storedEvent, isActive = store.GetActiveEvent()
	h.Equals(t, true, isActive)
	h.Equals(t, event1.EventID, storedEvent.EventID)
	h.Equals(t, event1.State, storedEvent.State)
}

func TestCancelInterruptionEvent(t *testing.T) {
	store := interruptioneventstore.New(config.Config{})

	event := &interruptionevent.InterruptionEvent{
		EventID:   "123",
		StartTime: time.Now(),
	}
	store.AddInterruptionEvent(event)

	store.CancelInterruptionEvent(event.EventID)

	storedEvent, isActive := store.GetActiveEvent()
	h.Equals(t, false, isActive)
	h.Assert(t, event.EventID != storedEvent.EventID,
		fmt.Sprintf("Event has not been canceled. Expected EventID '', but got %q", storedEvent.EventID))
}

func TestShouldDrainNode(t *testing.T) {
	store := interruptioneventstore.New(config.Config{})
	futureEvent := &interruptionevent.InterruptionEvent{
		EventID:   "future",
		StartTime: time.Now().Add(time.Second * 20),
	}
	store.AddInterruptionEvent(futureEvent)
	h.Equals(t, false, store.ShouldDrainNode())

	currentEvent := &interruptionevent.InterruptionEvent{
		EventID:   "current",
		StartTime: time.Now(),
	}
	store.AddInterruptionEvent(currentEvent)
	h.Equals(t, true, store.ShouldDrainNode())
}

func TestMarkAllAsDrained(t *testing.T) {
	store := interruptioneventstore.New(config.Config{})
	event1 := &interruptionevent.InterruptionEvent{
		EventID:   "1",
		StartTime: time.Now().Add(time.Second * 20),
		Drained:   false,
	}
	event2 := &interruptionevent.InterruptionEvent{
		EventID:   "2",
		StartTime: time.Now().Add(time.Second * 20),
		Drained:   false,
	}

	store.AddInterruptionEvent(event1)
	store.AddInterruptionEvent(event2)
	store.MarkAllAsDrained()

	// When events are marked as Drained=true, then they are no longer
	// returned by the GetActiveEvent func, so we expect false
	_, isActive := store.GetActiveEvent()
	h.Equals(t, false, isActive)
}

func TestShouldUncordonNode(t *testing.T) {
	eventID := "123"
	store := interruptioneventstore.New(config.Config{})
	h.Equals(t, false, store.ShouldUncordonNode())

	event := &interruptionevent.InterruptionEvent{
		EventID: eventID,
	}
	store.AddInterruptionEvent(event)
	h.Equals(t, false, store.ShouldUncordonNode())

	store.CancelInterruptionEvent(event.EventID)
	h.Equals(t, true, store.ShouldUncordonNode())

	store.IgnoreEvent(eventID)
	store.AddInterruptionEvent(event)
	h.Equals(t, true, store.ShouldUncordonNode())
}

func TestIgnoreEvent(t *testing.T) {
	eventID := "event-id-123"
	store := interruptioneventstore.New(config.Config{})
	store.IgnoreEvent("")
	event := &interruptionevent.InterruptionEvent{
		EventID:   eventID,
		State:     "active",
		StartTime: time.Now(),
	}
	store.AddInterruptionEvent(event)
	h.Equals(t, true, store.ShouldDrainNode())

	store.IgnoreEvent(eventID)
	h.Equals(t, false, store.ShouldDrainNode())
}

// BenchmarkDrainEventStore tests concurrent read/write patterns. We don't really care about the timings as long as deadlock doesn't occur
func BenchmarkDrainEventStore(b *testing.B) {
	idBound := 10
	store := interruptioneventstore.New(config.Config{})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			store.AddInterruptionEvent(&interruptionevent.InterruptionEvent{
				EventID:   strconv.Itoa(rand.Intn(idBound)),
				StartTime: time.Now(),
			})
			store.IgnoreEvent(strconv.Itoa(rand.Intn(idBound)))
			store.CancelInterruptionEvent(strconv.Itoa(rand.Intn(idBound)))
			store.GetActiveEvent()
			store.ShouldDrainNode()
			store.ShouldUncordonNode()
		}
	})
}
