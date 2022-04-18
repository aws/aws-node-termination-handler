/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TerminatorSpec defines the desired state of Terminator
type TerminatorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	MatchLabels map[string]string `json:"matchLabels,omitempty"`
	SQS         SQSSpec           `json:"sqs,omitempty"`
	Drain       DrainSpec         `json:"drain,omitempty"`
	Events      EventsSpec        `json:"events,omitempty"`
}

// SQSSpec defines inputs to SQS "receive messages" requests.
type SQSSpec struct {
	// https://pkg.go.dev/github.com/aws/aws-sdk-go@v1.38.55/service/sqs#ReceiveMessageInput
	QueueURL string `json:"queueURL,omitempty"`
}

// DrainSpec defines inputs to the cordon and drain operations.
type DrainSpec struct {
	// https://pkg.go.dev/k8s.io/kubectl@v0.21.4/pkg/drain#Helper
	Force               bool `json:"force,omitempty"`
	GracePeriodSeconds  int  `json:"gracePeriodSeconds,omitempty"`
	IgnoreAllDaemonSets bool `json:"ignoreAllDaemonSets,omitempty"`
	DeleteEmptyDirData  bool `json:"deleteEmptyDirData,omitempty"`
	TimeoutSeconds      int  `json:"timeoutSeconds,omitempty"`
}

type Action = string

var Actions = struct {
	CordonAndDrain,
	Cordon,
	NoAction Action
}{
	CordonAndDrain: "CordonAndDrain",
	Cordon:         "Cordon",
	NoAction:       "NoAction",
}

// EventsSpec defines the action(s) that should be performed in response to a particular event.
type EventsSpec struct {
	AutoScalingTermination  Action `json:"autoScalingTermination,omitempty"`
	RebalanceRecommendation Action `json:"rebalanceRecommendation,omitempty"`
	ScheduledChange         Action `json:"scheduledChange,omitempty"`
	SpotInterruption        Action `json:"spotInterruption,omitempty"`
	StateChange             Action `json:"stateChange,omitempty"`
}

// TerminatorStatus defines the observed state of Terminator
type TerminatorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// Terminator is the Schema for the terminators API
//+kubebuilder:object:root=true
//+kubebuilder:resource:path=terminators,scope=Cluster
//+kubebuilder:subresource:status
type Terminator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerminatorSpec   `json:"spec,omitempty"`
	Status TerminatorStatus `json:"status,omitempty"`
}

func (t *Terminator) SetDefaults(_ context.Context) {
	// Stubbed to satisfy interface requirements.
	// TODO: actually set defaults
}

// TerminatorList contains a list of Terminator
//+kubebuilder:object:root=true
type TerminatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Terminator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Terminator{}, &TerminatorList{})
}
