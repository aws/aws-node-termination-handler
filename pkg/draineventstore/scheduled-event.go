package draineventstore

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/drainer"
	"github.com/aws/aws-node-termination-handler/pkg/drainevent"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
)

const (
	// ScheduledEventKind is a const to define a scheduled event kind of drainable event
	ScheduledEventKind           = "SCHEDULED_EVENT"
	scheduledEventStateCompleted = "completed"
	scheduledEventStateCancelled = "cancelled"
	scheduledEventDateFormat     = "02 Jan 2006 15:04:05 GMT"
)

// MonitorForScheduledEvents continuously monitors metadata for scheduled events and sends drain events to the passed in channel
func MonitorForScheduledEvents(drainChan chan<- drainevent.DrainEvent, cancelChan chan<- drainevent.DrainEvent, nthConfig config.Config) {
	log.Println("Started monitoring for scheduled events")
	for range time.Tick(time.Second * 2) {
		drainEvents := CheckForScheduledEvents(nthConfig.MetadataURL)
		for _, drainEvent := range drainEvents {
			if !isStateCancelledOrCompleted(drainEvent.State) {
				log.Println("Sending drain events to the drain channel")
				drainChan <- drainEvent
				// cool down for the system to respond to the drain
				time.Sleep(120 * time.Second)
			} else if isStateCancelledOrCompleted(drainEvent.State) {
				log.Println("Sending cancel events to the cancel channel")
				cancelChan <- drainEvent
			}
		}
	}
}

// CheckForScheduledEvents Checks EC2 instance metadata for a scheduled event requiring a node drain
func CheckForScheduledEvents(metadataURL string) []drainevent.DrainEvent {
	events := make([]drainevent.DrainEvent, 0)
	resp, err := ec2metadata.RequestMetadata(metadataURL, ec2metadata.ScheduledEventPath)
	if err != nil {
		log.Fatalf("Unable to parse metadata response: %s", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Received an http error code when querying for scheduled events.")
		return events
	}
	var scheduledEvents []ec2metadata.ScheduledEventDetail
	json.NewDecoder(resp.Body).Decode(&scheduledEvents)
	for _, scheduledEvent := range scheduledEvents {
		preDrainFunc := func() {}
		if scheduledEvent.Code == ec2metadata.SystemRebootCode {
			preDrainFunc = uncordonAfterRebootPreDrain
		}

		events = append(events, drainevent.DrainEvent{
			EventID:      scheduledEvent.EventID,
			Kind:         ScheduledEventKind,
			Description:  fmt.Sprintf("%s will occur between %s and %s because %s\n", scheduledEvent.Code, scheduledEvent.NotBefore, scheduledEvent.NotAfter, scheduledEvent.Description),
			State:        scheduledEvent.State,
			StartTime:    parseScheduledEventTime(scheduledEvent.NotBefore),
			EndTime:      parseScheduledEventTime(scheduledEvent.NotAfter),
			PreDrainTask: preDrainFunc,
		})
	}
	return events
}

func uncordonAfterRebootPreDrain() {
	// if the node is already maked as unschedulable, then don't do anything
	unschedulable, err := drainer.IsNodeUnschedulable()
	if err == nil && unschedulable {
		log.Println("Node is already marked unschedulable, not taking any action to add uncordon label.")
		return
	} else if err != nil {
		log.Printf("Encountered an error while checking if the node is unschedulable. Not setting an uncordon label to be safe. Error: %v\n", err)
		return
	}
	// adds label to node so that the system will uncordon the node after the scheduled reboot has taken place
	err = drainer.AddNodeLabel(drainevent.ActionLabelKey, drainevent.UncordonAfterRebootLabelVal)
	if err != nil {
		log.Printf("Unable to label node with action to uncordon after system-reboot. Error: %v", err)
		return
	}
	// adds label with the current time which is checked against the uptime of the node when processing labels on startup
	err = drainer.AddNodeLabel(drainevent.ActionLabelTimeKey, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		log.Printf("Unable to label node with action time for uncordon after system-reboot. Error: %v", err)
		// if time can't be recorded, rollback the action label
		drainer.RemoveNodeLabel(drainevent.ActionLabelKey)
		return
	}
	log.Println("Successfully applied uncordon after reboot action label to node.")
}

func isStateCancelledOrCompleted(state string) bool {
	return state == scheduledEventStateCancelled ||
		state == scheduledEventStateCompleted
}

func parseScheduledEventTime(inputTime string) time.Time {
	scheduledTime, err := time.Parse(scheduledEventDateFormat, inputTime)
	if err != nil {
		log.Fatalln("Could not parse time from scheduled event metadata json", err.Error())
	}
	return scheduledTime
}
