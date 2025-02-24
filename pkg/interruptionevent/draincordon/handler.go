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

package draincordon

import (
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent/internal/common"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/api/errors"
)

var allowedKinds = []string{
	monitor.ASGLifecycleKind,
	monitor.RebalanceRecommendationKind,
	monitor.SQSTerminateKind,
	monitor.ScheduledEventKind,
	monitor.SpotITNKind,
	monitor.StateChangeKind,
}

type Handler struct {
	commonHandler *common.Handler
	nodeMetadata  ec2metadata.NodeMetadata
}

func New(interruptionEventStore *interruptioneventstore.Store, node node.Node, nthConfig config.Config, nodeMetadata ec2metadata.NodeMetadata, metrics observability.Metrics, recorder observability.K8sEventRecorder) *Handler {
	commonHandler := &common.Handler{
		InterruptionEventStore: interruptionEventStore,
		Node:                   node,
		NthConfig:              nthConfig,
		Metrics:                metrics,
		Recorder:               recorder,
	}

	return &Handler{
		commonHandler: commonHandler,
		nodeMetadata:  nodeMetadata,
	}
}

func (h *Handler) HandleEvent(drainEvent *monitor.InterruptionEvent) error {
	if !common.IsAllowedKind(drainEvent.Kind, allowedKinds...) {
		return nil
	}

	nodeFound := true
	nodeName, err := h.commonHandler.GetNodeName(drainEvent)
	if err != nil {
		return fmt.Errorf("get node name for instanceID=%s: %w", drainEvent.InstanceID, err)
	}

	nodeLabels, err := h.commonHandler.Node.GetNodeLabels(nodeName)
	if err != nil {
		log.Warn().
			Err(err).
			Interface("fallbackNodeLabels", drainEvent.NodeLabels).
			Str("nodeName", nodeName).
			Msg("Failed to get node labels. Proceeding with fallback labels")
		nodeFound = false
	} else {
		drainEvent.NodeLabels = nodeLabels
	}

	if drainEvent.PreDrainTask != nil {
		h.commonHandler.RunPreDrainTask(nodeName, drainEvent)
	}

	podNameList, err := h.commonHandler.Node.FetchPodNameList(nodeName)
	if err != nil {
		log.Warn().
			Err(err).
			Strs("fallbackPodNames", podNameList).
			Str("nodeName", nodeName).
			Msg("Failed to fetch pod names. Proceeding with fallback pod names")
	} else {
		drainEvent.Pods = podNameList
	}

	err = h.commonHandler.Node.LogPods(podNameList, nodeName)
	if err != nil {
		log.Warn().Err(err).Str("nodeName", nodeName).Msg("Failed to log pods")
	}

	if h.commonHandler.NthConfig.CordonOnly || (!h.commonHandler.NthConfig.EnableSQSTerminationDraining && drainEvent.IsRebalanceRecommendation() && !h.commonHandler.NthConfig.EnableRebalanceDraining) {
		err = h.cordonNode(nodeName, drainEvent)
	} else {
		err = h.cordonAndDrainNode(nodeName, drainEvent)
	}

	if h.commonHandler.NthConfig.WebhookURL != "" {
		webhook.Post(h.nodeMetadata, drainEvent, h.commonHandler.NthConfig)
	}

	if err != nil {
		h.commonHandler.InterruptionEventStore.CancelInterruptionEvent(drainEvent.EventID)
	} else {
		h.commonHandler.InterruptionEventStore.MarkAllAsProcessed(nodeName)
	}

	if (err == nil || (!nodeFound && h.commonHandler.NthConfig.DeleteSqsMsgIfNodeNotFound)) && drainEvent.PostDrainTask != nil {
		h.commonHandler.RunPostDrainTask(nodeName, drainEvent)
	}

	// Only add out-of-service taint if ENABLE_OUT_OF_SERVICE_TAINT flag is true, and CORDON_ONLY flag is false
	if err == nil && h.commonHandler.NthConfig.EnableOutOfServiceTaint && !h.commonHandler.NthConfig.CordonOnly {
		err = h.commonHandler.Node.TaintOutOfService(nodeName)
		if err != nil {
			return fmt.Errorf("cannot add out-of-service taint on node %s: %w", nodeName, err)
		}
	}

	return nil
}

func (h *Handler) cordonNode(nodeName string, drainEvent *monitor.InterruptionEvent) error {
	err := h.commonHandler.Node.Cordon(nodeName, drainEvent.Description)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Err(err).Msgf("node '%s' not found in the cluster", nodeName)
		} else {
			log.Err(err).Msg("There was a problem while trying to cordon the node")
			h.commonHandler.Recorder.Emit(nodeName, observability.Warning, observability.CordonErrReason, observability.CordonErrMsgFmt, err.Error())
		}
		return err
	} else {
		log.Info().Str("node_name", nodeName).Str("reason", drainEvent.Description).Msg("Node successfully cordoned")
		h.commonHandler.Metrics.NodeActionsInc("cordon", nodeName, drainEvent.EventID, err)
		h.commonHandler.Recorder.Emit(nodeName, observability.Normal, observability.CordonReason, observability.CordonMsg)
	}
	return nil
}

func (h *Handler) cordonAndDrainNode(nodeName string, drainEvent *monitor.InterruptionEvent) error {
	err := h.commonHandler.Node.CordonAndDrain(nodeName, drainEvent.Description, h.commonHandler.Recorder.EventRecorder)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Err(err).Msgf("node '%s' not found in the cluster", nodeName)
		} else {
			log.Err(err).Msg("There was a problem while trying to cordon and drain the node")
			h.commonHandler.Metrics.NodeActionsInc("cordon-and-drain", nodeName, drainEvent.EventID, err)
			h.commonHandler.Recorder.Emit(nodeName, observability.Warning, observability.CordonAndDrainErrReason, observability.CordonAndDrainErrMsgFmt, err.Error())
		}
		return err
	} else {
		log.Info().Str("node_name", nodeName).Str("reason", drainEvent.Description).Msg("Node successfully cordoned and drained")
		h.commonHandler.Metrics.NodeActionsInc("cordon-and-drain", nodeName, drainEvent.EventID, err)
		h.commonHandler.Recorder.Emit(nodeName, observability.Normal, observability.CordonAndDrainReason, observability.CordonAndDrainMsg)
	}
	return nil
}
