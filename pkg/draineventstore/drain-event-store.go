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
	NthConfig       *config.Config
	drainEventStore map[string]*drainevent.DrainEvent
	atLeastOneEvent bool
}

// NewStore Create a new drain event store
func NewStore(nthConfig *config.Config) *Store {
	return &Store{
		NthConfig:       nthConfig,
		drainEventStore: make(map[string]*drainevent.DrainEvent),
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
	s.atLeastOneEvent = true
	s.RLock()
	_, ok := s.drainEventStore[drainEvent.EventID]
	s.RUnlock()
	if ok {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.drainEventStore[drainEvent.EventID] = drainEvent
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

func (s *Store) shouldEventDrain(drainEvent *drainevent.DrainEvent) bool {
	if !drainEvent.Drained && s.TimeUntilDrain(drainEvent) <= 0 {
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
