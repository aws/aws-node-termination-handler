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

package launch

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent/internal/common"
	"github.com/aws/aws-node-termination-handler/pkg/interruptioneventstore"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-node-termination-handler/pkg/observability"
	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

const instanceIDLabel = "alpha.eksctl.io/instance-id"

type Handler struct {
	commonHandler *common.Handler
	clientset     *kubernetes.Clientset
}

func New(interruptionEventStore *interruptioneventstore.Store, node node.Node, nthConfig config.Config, metrics observability.Metrics, recorder observability.K8sEventRecorder, clientset *kubernetes.Clientset) *Handler {
	commonHandler := &common.Handler{
		InterruptionEventStore: interruptionEventStore,
		Node:                   node,
		NthConfig:              nthConfig,
		Metrics:                metrics,
		Recorder:               recorder,
	}

	return &Handler{
		commonHandler: commonHandler,
		clientset:     clientset,
	}
}

func (h *Handler) HandleEvent(drainEvent *monitor.InterruptionEvent) error {
	if drainEvent == nil {
		return fmt.Errorf("handling nil event")
	}

	if !common.IsAllowedKind(drainEvent.Kind, monitor.ASGLaunchLifecycleKind) {
		return nil
	}

	isNodeReady, err := h.isNodeReady(drainEvent.InstanceID)
	if err != nil {
		h.commonHandler.InterruptionEventStore.CancelInterruptionEvent(drainEvent.EventID)
		return fmt.Errorf("EC2 instance is not found and ready in cluster instanceID=%s: %w", drainEvent.InstanceID, err)
	}
	if !isNodeReady {
		h.commonHandler.InterruptionEventStore.CancelInterruptionEvent(drainEvent.EventID)
		return fmt.Errorf("EC2 instance is not found and ready in cluster instanceID=%s", drainEvent.InstanceID)
	}

	nodeName, err := h.commonHandler.GetNodeName(drainEvent)
	if err != nil {
		return fmt.Errorf("unable to retrieve node name for ASG event processing: %w", err)
	}

	if drainEvent.PostDrainTask != nil {
		h.commonHandler.RunPostDrainTask(nodeName, drainEvent)
	}
	return nil
}

func (h *Handler) isNodeReady(instanceID string) (bool, error) {
	nodes, err := h.getNodesWithInstanceID(instanceID)
	if err != nil {
		return false, fmt.Errorf("find node(s) with instanceId=%s: %w", instanceID, err)
	}

	if len(nodes) == 0 {
		log.Info().Str("instanceID", instanceID).Msg("EC2 instance not found in cluster")
		return false, nil
	}

	for _, node := range nodes {
		conditions := node.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == "Ready" && condition.Status != "True" {
				log.Info().Str("instanceID", instanceID).Msg("EC2 instance found, but not ready in cluster")
				return false, nil
			}
		}
	}
	log.Info().Str("instanceID", instanceID).Msg("EC2 instance is found and ready in cluster")
	return true, nil
}

// Gets Nodes connected to K8s cluster
func (h *Handler) getNodesWithInstanceID(instanceID string) ([]v1.Node, error) {
	nodes, err := h.getNodesWithInstanceFromLabel(instanceID)
	if err != nil {
		return nil, err
	}
	if len(nodes) != 0 {
		return nodes, nil
	}

	nodes, err = h.getNodesWithInstanceFromProviderID(instanceID)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func (h *Handler) getNodesWithInstanceFromLabel(instanceID string) ([]v1.Node, error) {
	instanceIDReq, err := labels.NewRequirement(instanceIDLabel, selection.Equals, []string{instanceID})
	if err != nil {
		return nil, fmt.Errorf("construct node search requirement %s=%s: %w", instanceIDLabel, instanceID, err)
	}
	selector := labels.NewSelector().Add(*instanceIDReq)
	nodeList, err := h.clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("list nodes using selector %q: %w", selector.String(), err)
	}
	return nodeList.Items, nil
}

func (h *Handler) getNodesWithInstanceFromProviderID(instanceID string) ([]v1.Node, error) {
	nodeList, err := h.clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list all nodes: %w", err)
	}

	var filteredNodes []v1.Node
	for _, node := range nodeList.Items {
		if !strings.Contains(node.Spec.ProviderID, instanceID) {
			continue
		}
		filteredNodes = append(filteredNodes, node)
	}
	return filteredNodes, nil
}
