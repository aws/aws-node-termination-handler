// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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
// permissions and limitations under the License.

package observability

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	kErr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// Kubernetes event types, reasons and messages
const (
	Normal                  = corev1.EventTypeNormal
	Warning                 = corev1.EventTypeWarning
	MonitorErrReason        = "MonitorError"
	MonitorErrMsgFmt        = "There was a problem monitoring for events in monitor '%s'"
	UncordonErrReason       = "UncordonError"
	UncordonErrMsgFmt       = "There was a problem while trying to uncordon the node: %s"
	UncordonReason          = "Uncordon"
	UncordonMsg             = "Node successfully uncordoned"
	PreDrainErrReason       = "PreDrainError"
	PreDrainErrMsgFmt       = "There was a problem executing the pre-drain task: %s"
	PreDrainReason          = "PreDrain"
	PreDrainMsg             = "Pre-drain task successfully executed"
	CordonErrReason         = "CordonError"
	CordonErrMsgFmt         = "There was a problem while trying to cordon the node: %s"
	CordonReason            = "Cordon"
	CordonMsg               = "Node successfully cordoned"
	CordonAndDrainErrReason = "CordonAndDrainError"
	CordonAndDrainErrMsgFmt = "There was a problem while trying to cordon and drain the node: %s"
	CordonAndDrainReason    = "CordonAndDrain"
	CordonAndDrainMsg       = "Node successfully cordoned and drained"
	PostDrainErrReason      = "PostDrainError"
	PostDrainErrMsgFmt      = "There was a problem executing the post-drain task: %s"
	PostDrainReason         = "PostDrain"
	PostDrainMsg            = "Post-drain task successfully executed"
)

// Interruption event reasons
const (
	scheduledEventReason          = "ScheduledEvent"
	spotITNReason                 = "SpotInterruption"
	sqsTerminationReason          = "SQSTermination"
	rebalanceRecommendationReason = "RebalanceRecommendation"
	stateChangeReason             = "StateChange"
	asgLifecycleReason            = "ASGLifecycle"
	unknownReason                 = "UnknownInterruption"
)

// K8sEventRecorder wraps a Kubernetes event recorder with some extra information
type K8sEventRecorder struct {
	annotations map[string]string
	clientSet   *kubernetes.Clientset
	enabled     bool
	sqsMode     bool
	record.EventRecorder
}

// InitK8sEventRecorder creates a Kubernetes event recorder
func InitK8sEventRecorder(enabled bool, nodeName string, sqsMode bool, nodeMetadata ec2metadata.NodeMetadata, extraAnnotationsStr string, clientSet *kubernetes.Clientset) (K8sEventRecorder, error) {
	if !enabled {
		return K8sEventRecorder{}, nil
	}

	annotations := make(map[string]string)
	annotations["account-id"] = nodeMetadata.AccountId
	if !sqsMode {
		annotations["availability-zone"] = nodeMetadata.AvailabilityZone
		annotations["instance-id"] = nodeMetadata.InstanceID
		annotations["instance-life-cycle"] = nodeMetadata.InstanceLifeCycle
		annotations["instance-type"] = nodeMetadata.InstanceType
		annotations["local-hostname"] = nodeMetadata.LocalHostname
		annotations["local-ipv4"] = nodeMetadata.LocalIP
		annotations["public-hostname"] = nodeMetadata.PublicHostname
		annotations["public-ipv4"] = nodeMetadata.PublicIP
		annotations["region"] = nodeMetadata.Region
	}

	var err error
	if extraAnnotationsStr != "" {
		annotations, err = parseExtraAnnotations(annotations, extraAnnotationsStr)
		if err != nil {
			return K8sEventRecorder{}, err
		}
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientSet.CoreV1().Events("")})

	return K8sEventRecorder{
		annotations: annotations,
		clientSet:   clientSet,
		enabled:     true,
		sqsMode:     sqsMode,
		EventRecorder: broadcaster.NewRecorder(
			scheme.Scheme,
			corev1.EventSource{
				Component: "aws-node-termination-handler",
				Host:      nodeName,
			},
		),
	}, nil
}

// Emit a Kubernetes event for the given node and with the given event type, reason and message
func (r K8sEventRecorder) Emit(nodeName string, eventType, eventReason, eventMsgFmt string, eventMsgArgs ...interface{}) {
	if r.enabled {
		var node *corev1.Node
		var annotations map[string]string
		if r.sqsMode {
			var err error
			node, err = r.clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
			if err != nil {
				if kErr.IsNotFound(err) {
					return
				}
				log.Err(err).Msg("Emitting Kubernetes event failed")
				return
			}
			annotations = generateNodeAnnotations(node, r.annotations)
		} else {
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nodeName,
					Namespace: "default",
				},
			}
			annotations = r.annotations
		}
		r.AnnotatedEventf(node, annotations, eventType, eventReason, eventMsgFmt, eventMsgArgs...) //nolint:all
	}
}

// getReasonForKindV1 returns a Kubernetes event reason for the given interruption event kind.
// Compatible with log format version 1.
func getReasonForKindV1(eventKind, monitorKind string) string {
	// In v1 all events received from SQS were given the same reason.
	if monitorKind == monitor.SQSTerminateKind {
		return sqsTerminationReason
	}

	// However, events received from IMDS could be more specific.
	switch eventKind {
	case monitor.ScheduledEventKind:
		return scheduledEventReason
	case monitor.SpotITNKind:
		return spotITNReason
	case monitor.RebalanceRecommendationKind:
		return rebalanceRecommendationReason
	case monitor.StateChangeKind:
		return stateChangeReason
	case monitor.ASGLifecycleKind:
		return asgLifecycleReason
	default:
		return unknownReason
	}
}

// getReasonForKindV2 returns a Kubernetes event reason for the given interruption event kind.
// Compatible with log format version 2.
func getReasonForKindV2(eventKind, _ string) string {
	// v2 added reasons for more event kinds for both IMDS and SQS events.
	switch eventKind {
	case monitor.ScheduledEventKind:
		return scheduledEventReason
	case monitor.SpotITNKind:
		return spotITNReason
	case monitor.RebalanceRecommendationKind:
		return rebalanceRecommendationReason
	case monitor.StateChangeKind:
		return stateChangeReason
	case monitor.ASGLifecycleKind:
		return asgLifecycleReason
	default:
		return unknownReason
	}
}

var GetReasonForKind func(kind, monitor string) string = getReasonForKindV1

func SetReasonForKindVersion(version int) error {
	switch version {
	case 1:
		GetReasonForKind = getReasonForKindV1
		return nil
	case 2:
		GetReasonForKind = getReasonForKindV2
		return nil
	default:
		GetReasonForKind = getReasonForKindV1
		return fmt.Errorf("Unrecognized 'reason for kind' version: %d, using version 1", version)
	}
}

// Parse the given extra annotations string into a map
func parseExtraAnnotations(annotations map[string]string, extraAnnotationsStr string) (map[string]string, error) {
	parts := strings.Split(extraAnnotationsStr, ",")
	for _, part := range parts {
		keyValue := strings.Split(part, "=")
		if len(keyValue) != 2 {
			return nil, fmt.Errorf("error parsing annotations")
		}
		annotations[keyValue[0]] = keyValue[1]
	}
	return annotations, nil
}

// Generate annotations for an event occurred on the given node
func generateNodeAnnotations(node *corev1.Node, annotations map[string]string) map[string]string {
	nodeAnnotations := make(map[string]string)
	for k, v := range annotations {
		nodeAnnotations[k] = v
	}
	nodeAnnotations["availability-zone"] = node.Labels["topology.kubernetes.io/zone"]
	nodeAnnotations["instance-id"] = node.Spec.ProviderID[strings.LastIndex(node.Spec.ProviderID, "/")+1:]
	nodeAnnotations["instance-type"] = node.Labels["node.kubernetes.io/instance-type"]
	nodeAnnotations["local-hostname"] = node.Name
	for _, address := range node.Status.Addresses {
		// If there's more than one address of the same type, use the first one
		switch address.Type {
		case corev1.NodeInternalIP:
			if _, exist := annotations["local-ipv4"]; !exist {
				nodeAnnotations["local-ipv4"] = address.Address
			}
		case corev1.NodeExternalDNS:
			if _, exist := annotations["public-hostname"]; !exist {
				nodeAnnotations["public-hostname"] = address.Address
			}
		case corev1.NodeExternalIP:
			if _, exist := annotations["public-ipv4"]; !exist {
				nodeAnnotations["public-ipv4"] = address.Address
			}
		}
	}
	nodeAnnotations["region"] = node.Labels["topology.kubernetes.io/region"]

	return nodeAnnotations
}
