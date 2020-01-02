package drainevent

import (
	"time"
)

const (
	// ActionLabelKey is a k8s label key that can be added to the k8s node NTH is running on
	ActionLabelKey = "aws-node-termination-handler/action"
	// ActionLabelTimeKey is a k8s label key whose value is the secs since the epoch when an action label is added
	ActionLabelTimeKey = "aws-node-termination-handler/action-time"
	// UncordonAfterRebootLabelVal is a k8s label value that can added to an action label to uncordon a node
	UncordonAfterRebootLabelVal = "UncordonAfterReboot"
)

// DrainEvent gives more context of the drainable event
type DrainEvent struct {
	EventID      string
	Kind         string
	Description  string
	State        string
	StartTime    time.Time
	EndTime      time.Time
	PreDrainTask func() `json:"-"`
	Drained      bool
}

// TimeUntilEvent returns the duration until the event start time
func (e *DrainEvent) TimeUntilEvent() time.Duration {
	return e.StartTime.Sub(time.Now())
}
