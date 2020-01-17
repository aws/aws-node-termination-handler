package draineventstore

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
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
func MonitorForScheduledEvents(drainChan chan<- drainevent.DrainEvent, nthConfig config.Config) {
	log.Println("Started monitoring for scheduled events")
	for range time.Tick(time.Second * 2) {
		drainEvents := CheckForScheduledEvents(nthConfig.MetadataURL)
		for _, drainEvent := range drainEvents {
			if !isStateCancelledOrCompleted(drainEvent.State) {
				log.Println("Sending drain events to the drain channel")
				drainChan <- drainEvent
				// cool down for the system to respond to the drain
				time.Sleep(120 * time.Second)
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
		events = append(events, drainevent.DrainEvent{
			EventID:     scheduledEvent.EventID,
			Kind:        ScheduledEventKind,
			Description: fmt.Sprintf("%s will occur between %s and %s because %s\n", scheduledEvent.Code, scheduledEvent.NotBefore, scheduledEvent.NotAfter, scheduledEvent.Description),
			State:       scheduledEvent.State,
			StartTime:   parseScheduledEventTime(scheduledEvent.NotBefore),
			EndTime:     parseScheduledEventTime(scheduledEvent.NotAfter),
		})
	}
	return events
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
