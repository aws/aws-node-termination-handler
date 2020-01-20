package drainevent

import (
	"time"
)

// DrainEvent gives more context of the drainable event
type DrainEvent struct {
	EventID     string
	Kind        string
	Description string
	State       string
	StartTime   time.Time
	EndTime     time.Time
	Drained     bool
}

// TimeUntilEvent returns the duration until the event start time
func (e *DrainEvent) TimeUntilEvent() time.Duration {
	return e.StartTime.Sub(time.Now())
}
