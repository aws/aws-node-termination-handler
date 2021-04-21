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
// permissions and limitations under the License.

package observability

import (
	"fmt"
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/rebalancerecommendation"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/scheduledevent"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/spotitn"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
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
	sqsTerminateReason            = "SQSTermination"
	rebalanceRecommendationReason = "RebalanceRecommendation"
	unknownReason                 = "UnknownInterruption"
)

// K8sEventRecorder wraps a Kubernetes event recorder with some extra information
type K8sEventRecorder struct {
	annotations map[string]string
	enabled     bool
	node        *corev1.Node
	record.EventRecorder
}

// InitK8sEventRecorder creates a Kubernetes event recorder
func InitK8sEventRecorder(enabled bool, nodeName string, nodeMetadata ec2metadata.NodeMetadata, extraAnnotationsStr string) (K8sEventRecorder, error) {
	if !enabled {
		return K8sEventRecorder{}, nil
	}

	// Create default annotations
	// Worth iterating over nodeMetadata fields using reflect? (trutx)
	annotations := make(map[string]string)
	annotations["account-id"] = nodeMetadata.AccountId
	annotations["availability-zone"] = nodeMetadata.AvailabilityZone
	annotations["instance-id"] = nodeMetadata.InstanceID
	annotations["instance-type"] = nodeMetadata.InstanceType
	annotations["local-hostname"] = nodeMetadata.LocalHostname
	annotations["local-ipv4"] = nodeMetadata.LocalIP
	annotations["public-hostname"] = nodeMetadata.PublicHostname
	annotations["public-ipv4"] = nodeMetadata.PublicIP
	annotations["region"] = nodeMetadata.Region

	// Parse extra annotations
	var err error
	if extraAnnotationsStr != "" {
		annotations, err = parseExtraAnnotations(annotations, extraAnnotationsStr)
		if err != nil {
			return K8sEventRecorder{}, err
		}
	}

	// Get in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return K8sEventRecorder{}, err
	}

	// Create clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return K8sEventRecorder{}, err
	}

	// Get node
	node, err := clientSet.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return K8sEventRecorder{}, err
	}

	// Create broadcaster
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientSet.CoreV1().Events("default")})

	// Create event recorder
	return K8sEventRecorder{
		annotations: annotations,
		enabled:     true,
		node:        node,
		EventRecorder: broadcaster.NewRecorder(
			scheme.Scheme,
			corev1.EventSource{
				Component: "aws-node-termination-handler",
				Host:      nodeName,
			},
		),
	}, nil
}

// Emit a Kubernetes event for the current node and with the given event type, reason and message
func (r K8sEventRecorder) Emit(eventType, eventReason, eventMsgFmt string, eventMsgArgs ...interface{}) {
	if r.enabled {
		r.AnnotatedEventf(r.node, r.annotations, eventType, eventReason, eventMsgFmt, eventMsgArgs...)
	}
}

// GetReasonForKind returns a Kubernetes event reason for the given interruption event kind
func GetReasonForKind(kind string) string {
	switch kind {
	case scheduledevent.ScheduledEventKind:
		return scheduledEventReason
	case spotitn.SpotITNKind:
		return spotITNReason
	case sqsevent.SQSTerminateKind:
		return sqsTerminateReason
	case rebalancerecommendation.RebalanceRecommendationKind:
		return rebalanceRecommendationReason
	default:
		return unknownReason
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
