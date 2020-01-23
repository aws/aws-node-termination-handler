package drainevent

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/node"
)

const (
	// ScheduledEventKind is a const to define a scheduled event kind of drainable event
	ScheduledEventKind           = "SCHEDULED_EVENT"
	scheduledEventStateCompleted = "completed"
	scheduledEventStateCancelled = "cancelled"
	scheduledEventDateFormat     = "02 Jan 2006 15:04:05 GMT"
)

// MonitorForScheduledEvents continuously monitors metadata for scheduled events and sends drain events to the passed in channel
func MonitorForScheduledEvents(drainChan chan<- DrainEvent, cancelChan chan<- DrainEvent, nthConfig config.Config) {
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
func CheckForScheduledEvents(metadataURL string) []DrainEvent {
	events := make([]DrainEvent, 0)
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
		var preDrainFunc preDrainTask = nil
		if scheduledEvent.Code == ec2metadata.SystemRebootCode {
			preDrainFunc = uncordonAfterRebootPreDrain
		}
		events = append(events, DrainEvent{
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

func uncordonAfterRebootPreDrain(node *node.Node) error {
	// if the node is already maked as unschedulable, then don't do anything
	unschedulable, err := node.IsUnschedulable()
	if err == nil && unschedulable {
		log.Println("Node is already marked unschedulable, not taking any action to add uncordon label.")
		return nil
	} else if err != nil {
		return fmt.Errorf("Encountered an error while checking if the node is unschedulable. Not setting an uncordon label: %w", err)
	}
	err = node.MarkForUncordonAfterReboot()
	if err != nil {
		return fmt.Errorf("Unable to mark the node for uncordon: %w", err)
	}
	log.Println("Successfully applied uncordon after reboot action label to node.")
	return nil
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
