package drainevent

import (
	"sync"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
)

// DrainEvent gives more context of the drainable event
type DrainEvent struct {
	EventID     string
	Kind        string
	Description string
	State       string
	StartTime   time.Time

	drained bool
}

// Store is a the drain event store data structure
type Store struct {
	sync.RWMutex
	NthConfig       *config.Config
	drainEventStore map[string]*DrainEvent
	atLeastOneEvent bool
}

// TimeUntilEvent returns the duration until the event start time
func (e *DrainEvent) TimeUntilEvent() time.Duration {
	return e.StartTime.Sub(time.Now())
}

// NewStore Create a new drain event store
func NewStore(nthConfig *config.Config) *Store {
	return &Store{
		NthConfig:       nthConfig,
		drainEventStore: make(map[string]*DrainEvent),
	}
}

// CancelDrainEvent removes a drain event from the internal store
func (s *Store) CancelDrainEvent(eventID string) {
	s.Lock()
	defer s.Unlock()
	delete(s.drainEventStore, eventID)
}

// AddDrainEvent adds a drain event to the internal store
func (s *Store) AddDrainEvent(drainEvent *DrainEvent) {
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

func (s *Store) shouldEventDrain(drainEvent *DrainEvent) bool {
	if !drainEvent.drained && s.TimeUntilDrain(drainEvent) <= 0 {
		return true
	}
	return false
}

// TimeUntilDrain returns the duration until a node drain should occur (can return a negative duration)
func (s *Store) TimeUntilDrain(drainEvent *DrainEvent) time.Duration {
	nodeTerminationGracePeriod := time.Duration(s.NthConfig.NodeTerminationGracePeriod) * time.Second
	drainTime := drainEvent.StartTime.Add(-1 * nodeTerminationGracePeriod)
	return drainTime.Sub(time.Now())
}

// MarkAllAsDrained should be called after the node has been drained to prevent further unnecessary drain calls to the k8s api
func (s *Store) MarkAllAsDrained() {
	s.Lock()
	defer s.Unlock()
	for _, drainEvent := range s.drainEventStore {
		drainEvent.drained = true
	}
}
