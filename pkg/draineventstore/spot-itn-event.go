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
	// SpotITNKind is a const to define a Spot ITN kind of drainable event
	SpotITNKind = "SPOT_ITN"
)

// MonitorForSpotITNEvents continuously monitors metadata for spot ITNs and sends drain events to the passed in channel
func MonitorForSpotITNEvents(drainChan chan<- drainevent.DrainEvent, cancelChan chan<- drainevent.DrainEvent, nthConfig config.Config) {
	log.Println("Started monitoring for spot ITN events")
	for range time.Tick(time.Second * 2) {
		drainEvent := checkForSpotInterruptionNotice(nthConfig.MetadataURL)
		if drainEvent.Kind == SpotITNKind {
			log.Println("Sending drain event to the drain channel")
			drainChan <- *drainEvent
			// cool down for the system to respond to the drain
			time.Sleep(120 * time.Second)
		}
	}
}

// checkForSpotInterruptionNotice Checks EC2 instance metadata for a spot interruption termination notice
func checkForSpotInterruptionNotice(metadataURL string) *drainevent.DrainEvent {
	resp, err := ec2metadata.RequestMetadata(metadataURL, ec2metadata.SpotInstanceActionPath)
	if err != nil {
		log.Fatalf("Unable to parse metadata response: %s", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Received an http error code when querying for spot itn events.")
		return &drainevent.DrainEvent{}
	}
	var instanceAction ec2metadata.InstanceAction
	json.NewDecoder(resp.Body).Decode(&instanceAction)
	interruptionTime, err := time.Parse(time.RFC3339, instanceAction.Time)
	if err != nil {
		log.Fatalln("Could not parse time from spot interruption notice metadata json", err.Error())
	}
	return &drainevent.DrainEvent{
		EventID:     instanceAction.Id,
		Kind:        SpotITNKind,
		StartTime:   interruptionTime,
		Description: fmt.Sprintf("Spot ITN received. %s will be %s at %s \n", instanceAction.Detail.InstanceId, instanceAction.Detail.InstanceAction, instanceAction.Time),
	}
}
