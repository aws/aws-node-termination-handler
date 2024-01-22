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
// permissions and limitations under the License

package common

import (
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	InterruptionEventStore *interruptioneventstore.Store
	Node                   node.Node
	NthConfig              config.Config
	Metrics                observability.Metrics
	Recorder               observability.K8sEventRecorder
}

func (h *Handler) GetNodeName(drainEvent *monitor.InterruptionEvent) (string, error) {
	if !h.NthConfig.UseProviderId {
		return drainEvent.NodeName, nil
	}

	nodeName, err := h.Node.GetNodeNameFromProviderID(drainEvent.ProviderID)
	if err != nil {
		return "", fmt.Errorf("parse node name from providerID=%q: %w", drainEvent.ProviderID, err)
	}
	return nodeName, nil
}

func (h *Handler) RunPreDrainTask(nodeName string, drainEvent *monitor.InterruptionEvent) {
	err := drainEvent.PreDrainTask(*drainEvent, h.Node)
	if err != nil {
		log.Err(err).Msg("There was a problem executing the pre-drain task")
		h.Recorder.Emit(nodeName, observability.Warning, observability.PreDrainErrReason, observability.PreDrainErrMsgFmt, err.Error())
	} else {
		h.Recorder.Emit(nodeName, observability.Normal, observability.PreDrainReason, observability.PreDrainMsg)
	}
	h.Metrics.NodeActionsInc("pre-drain", nodeName, drainEvent.EventID, err)
}

func (h *Handler) RunPostDrainTask(nodeName string, drainEvent *monitor.InterruptionEvent) {
	err := drainEvent.PostDrainTask(*drainEvent, h.Node)
	if err != nil {
		log.Err(err).Msg("There was a problem executing the post-drain task")
		h.Recorder.Emit(nodeName, observability.Warning, observability.PostDrainErrReason, observability.PostDrainErrMsgFmt, err.Error())
	} else {
		h.Recorder.Emit(nodeName, observability.Normal, observability.PostDrainReason, observability.PostDrainMsg)
	}
	h.Metrics.NodeActionsInc("post-drain", nodeName, drainEvent.EventID, err)
}

func IsAllowedKind(kind string, allowedKinds ...string) bool {
	for _, allowedKind := range allowedKinds {
		if kind == allowedKind {
			return true
		}
	}
	return false
}
