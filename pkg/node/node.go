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

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
)

const (
	// UncordonAfterRebootLabelVal is a k8s label value that can added to an action label to uncordon a node
	UncordonAfterRebootLabelVal = "UncordonAfterReboot"
	// ActionLabelKey is a k8s label key that can be added to the k8s node NTH is running on
	ActionLabelKey = "aws-node-termination-handler/action"
	// ActionLabelTimeKey is a k8s label key whose value is the secs since the epoch when an action label is added
	ActionLabelTimeKey = "aws-node-termination-handler/action-time"
	// EventIDLabelKey is a k8s label key whose value is the drainable event id
	EventIDLabelKey = "aws-node-termination-handler/event-id"
)

const (
	// SpotInterruptionTaint is a taint used to make spot instance unschedulable
	SpotInterruptionTaint = "aws-node-termination-handler/spot-itn"
	// ScheduledMaintenanceTaint is a taint used to make spot instance unschedulable
	ScheduledMaintenanceTaint = "aws-node-termination-handler/scheduled-maintenance"
	// ASGLifecycleTerminationTaint is a taint used to make instances about to be shutdown by ASG unschedulable
	ASGLifecycleTerminationTaint = "aws-node-termination-handler/asg-lifecycle-termination"
	// RebalanceRecommendationTaint is a taint used to make spot instance unschedulable
	RebalanceRecommendationTaint = "aws-node-termination-handler/rebalance-recommendation"

	maxTaintValueLength = 63
)

var (
	maxRetryDeadline      time.Duration = 5 * time.Second
	conflictRetryInterval time.Duration = 750 * time.Millisecond
)

// Node represents a kubernetes node with functions to manipulate its state via the kubernetes api server
type Node struct {
	nthConfig   config.Config
	drainHelper *drain.Helper
	uptime      uptime.UptimeFuncType
}

// New will construct a node struct to perform various node function through the kubernetes api server
func New(nthConfig config.Config) (*Node, error) {
	drainHelper, err := getDrainHelper(nthConfig)
	if err != nil {
		return nil, err
	}
	return NewWithValues(nthConfig, drainHelper, getUptimeFunc(nthConfig.UptimeFromFile))
}

// NewWithValues will construct a node struct with a drain helper and an uptime function
func NewWithValues(nthConfig config.Config, drainHelper *drain.Helper, uptime uptime.UptimeFuncType) (*Node, error) {
	return &Node{
		nthConfig:   nthConfig,
		drainHelper: drainHelper,
		uptime:      uptime,
	}, nil
}

// CordonAndDrain will cordon the node and evict pods based on the config
func (n Node) CordonAndDrain(nodeName string, reason string) error {
	if n.nthConfig.DryRun {
		log.Info().Str("node_name", nodeName).Str("reason", reason).Msg("Node would have been cordoned and drained, but dry-run flag was set.")
		return nil
	}
	err := n.Cordon(nodeName, reason)
	if err != nil {
		return err
	}
	// Delete all pods on the node
	log.Info().Msg("Draining the node")
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return err
	}
	err = drain.RunNodeDrain(n.drainHelper, node.Name)
	if err != nil {
		return err
	}
	return nil
}

// Cordon will add a NoSchedule on the node
func (n Node) Cordon(nodeName string, reason string) error {
	if n.nthConfig.DryRun {
		log.Info().Str("node_name", nodeName).Str("reason", reason).Msgf("Node would have been cordoned, but dry-run flag was set")
		return nil
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return err
	}
	err = drain.RunCordonOrUncordon(n.drainHelper, node, true)
	if err != nil {
		return err
	}
	return nil
}

// Uncordon will remove the NoSchedule on the node
func (n Node) Uncordon(nodeName string) error {
	if n.nthConfig.DryRun {
		log.Info().Str("node_name", nodeName).Msg("Node would have been uncordoned, but dry-run flag was set")
		return nil
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("There was an error fetching the node in preparation for uncordoning: %w", err)
	}
	err = drain.RunCordonOrUncordon(n.drainHelper, node, false)
	if err != nil {
		return err
	}
	return nil
}

// IsUnschedulable checks if the node is marked as unschedulable
func (n Node) IsUnschedulable(nodeName string) (bool, error) {
	if n.nthConfig.DryRun {
		log.Info().Msg("IsUnschedulable returning false since dry-run is set")
		return false, nil
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return true, err
	}
	return node.Spec.Unschedulable, nil
}

// MarkWithEventID will add the drain event ID to the node to be properly ignored after a system restart event
func (n Node) MarkWithEventID(nodeName string, eventID string) error {
	err := n.addLabel(nodeName, EventIDLabelKey, eventID)
	if err != nil {
		return fmt.Errorf("Unable to label node with event ID %s=%s: %w", EventIDLabelKey, eventID, err)
	}
	return nil
}

// RemoveNTHLabels will remove all the custom NTH labels added to the node
func (n Node) RemoveNTHLabels(nodeName string) error {
	for _, label := range []string{EventIDLabelKey, ActionLabelKey, ActionLabelTimeKey} {
		err := n.removeLabel(nodeName, label)
		if err != nil {
			return fmt.Errorf("Unable to remove %s from node: %w", label, err)
		}
	}
	return nil
}

// GetEventID will retrieve the event ID value from the node label
func (n Node) GetEventID(nodeName string) (string, error) {
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return "", fmt.Errorf("Could not get event ID label from node: %w", err)
	}
	val, ok := node.Labels[EventIDLabelKey]
	if n.nthConfig.DryRun && !ok {
		log.Warn().Msgf("Would have returned Error: 'Event ID Label %s was not found on the node', but dry-run flag was set", EventIDLabelKey)
		return "", nil
	}
	if !ok {
		return "", fmt.Errorf("Event ID Label %s was not found on the node", EventIDLabelKey)
	}
	return val, nil
}

// MarkForUncordonAfterReboot adds labels to the kubernetes node which NTH will read upon reboot
func (n Node) MarkForUncordonAfterReboot(nodeName string) error {
	// adds label to node so that the system will uncordon the node after the scheduled reboot has taken place
	err := n.addLabel(nodeName, ActionLabelKey, UncordonAfterRebootLabelVal)
	if err != nil {
		return fmt.Errorf("Unable to label node with action to uncordon after system-reboot: %w", err)
	}
	// adds label with the current time which is checked against the uptime of the node when processing labels on startup
	err = n.addLabel(nodeName, ActionLabelTimeKey, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		// if time can't be recorded, rollback the action label
		err := n.removeLabel(nodeName, ActionLabelKey)
		errMsg := "Unable to label node with action time for uncordon after system-reboot"
		if err != nil {
			return fmt.Errorf("%s and unable to rollback action label \"%s\": %w", errMsg, ActionLabelKey, err)
		}
		return fmt.Errorf("Unable to label node with action time for uncordon after system-reboot: %w", err)
	}
	return nil
}

// addLabel will add a label to the node given a label key and value
func (n Node) addLabel(nodeName string, key string, value string) error {
	type metadata struct {
		Labels map[string]string `json:"labels"`
	}
	type patch struct {
		Metadata metadata `json:"metadata"`
	}
	labels := make(map[string]string)
	labels[key] = value
	payload := patch{
		Metadata: metadata{
			Labels: labels,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("An error occurred while marshalling the json to add a label to the node: %w", err)
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return err
	}
	if n.nthConfig.DryRun {
		log.Info().Msgf("Would have added label (%s=%s) to node %s, but dry-run flag was set", key, value, nodeName)
		return nil
	}
	_, err = n.drainHelper.Client.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.StrategicMergePatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("%v node Patch failed when adding a label to the node: %w", node.Name, err)
	}
	return nil
}

// removeLabel will remove a node label given a label key
func (n Node) removeLabel(nodeName string, key string) error {
	type patchRequest struct {
		Op   string `json:"op"`
		Path string `json:"path"`
	}

	var patchReqs []interface{}
	patchRemove := patchRequest{
		Op:   "remove",
		Path: fmt.Sprintf("/metadata/labels/%s", jsonPatchEscape(key)),
	}
	payload, err := json.Marshal(append(patchReqs, patchRemove))
	if err != nil {
		return fmt.Errorf("An error occurred while marshalling the json to remove a label from the node: %w", err)
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return err
	}
	if n.nthConfig.DryRun {
		log.Info().Msgf("Would have removed label with key %s from node %s, but dry-run flag was set", key, nodeName)
		return nil
	}
	_, err = n.drainHelper.Client.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.JSONPatchType, payload, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("%v node Patch failed when removing a label from the node: %w", node.Name, err)
	}
	return nil
}

// GetNodeLabels will fetch node labels for a given nodeName
func (n Node) GetNodeLabels(nodeName string) (map[string]string, error) {
	if n.nthConfig.DryRun {
		log.Info().Str("node_name", nodeName).Msg("Node labels would have been fetched, but dry-run flag was set")
		return nil, nil
	}
	node, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return nil, err
	}
	return node.Labels, nil
}

// TaintSpotItn adds the spot termination notice taint onto a node
func (n Node) TaintSpotItn(nodeName string, eventID string) error {
	if !n.nthConfig.TaintNode {
		return nil
	}

	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n, SpotInterruptionTaint, eventID, corev1.TaintEffectNoSchedule)
}

// TaintASGLifecycleTermination adds the spot termination notice taint onto a node
func (n Node) TaintASGLifecycleTermination(nodeName string, eventID string) error {
	if !n.nthConfig.TaintNode {
		return nil
	}

	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n, ASGLifecycleTerminationTaint, eventID, corev1.TaintEffectNoSchedule)
}

// TaintRebalanceRecommendation adds the rebalance recommendation notice taint onto a node
func (n Node) TaintRebalanceRecommendation(nodeName string, eventID string) error {
	if !n.nthConfig.TaintNode {
		return nil
	}

	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n, RebalanceRecommendationTaint, eventID, corev1.TaintEffectNoSchedule)
}

// LogPods logs all the pod names on a node
func (n Node) LogPods(podList []string, nodeName string) error {
	podNamesArr := zerolog.Arr()
	for _, pod := range podList {
		podNamesArr = podNamesArr.Str(pod)
	}
	log.Info().Array("pod_names", podNamesArr).Str("node_name", nodeName).Msg("Pods on node")

	return nil
}

// FetchPodNameList fetches list of all the pods names running on given nodeName
func (n Node) FetchPodNameList(nodeName string) ([]string, error) {
	podList, err := n.fetchAllPods(nodeName)
	if err != nil {
		return nil, err
	}
	var podNamesList []string
	for _, pod := range podList.Items {
		podNamesList = append(podNamesList, pod.Name)
	}
	return podNamesList, nil
}

// TaintScheduledMaintenance adds the scheduled maintenance taint onto a node
func (n Node) TaintScheduledMaintenance(nodeName string, eventID string) error {
	if !n.nthConfig.TaintNode {
		return nil
	}

	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n, ScheduledMaintenanceTaint, eventID, corev1.TaintEffectNoSchedule)
}

// RemoveNTHTaints removes NTH-specific taints from a node
func (n Node) RemoveNTHTaints(nodeName string) error {
	if !n.nthConfig.TaintNode {
		return nil
	}

	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	taints := []string{SpotInterruptionTaint, ScheduledMaintenanceTaint, ASGLifecycleTerminationTaint, RebalanceRecommendationTaint}

	for _, taint := range taints {
		_, err = removeTaint(k8sNode, n.drainHelper.Client, taint)
		if err != nil {
			return fmt.Errorf("Unable to clean taint %s from node %s", taint, nodeName)
		}
	}

	return nil
}

// IsLabeledWithAction will return true if the current node is labeled with NTH action labels
func (n Node) IsLabeledWithAction(nodeName string) (bool, error) {
	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return false, fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}
	_, actionLabelOK := k8sNode.Labels[ActionLabelKey]
	_, eventIDLabelOK := k8sNode.Labels[EventIDLabelKey]
	return actionLabelOK && eventIDLabelOK, nil
}

// UncordonIfRebooted will check for node labels to trigger an uncordon because of a system-reboot scheduled event
func (n Node) UncordonIfRebooted(nodeName string) error {
	// TODO: this logic needs to be updated to dynamically look up the last reboot
	// w/ the ec2 api if the nodeName is not local.
	k8sNode, err := n.fetchKubernetesNode(nodeName)
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}
	timeVal, ok := k8sNode.Labels[ActionLabelTimeKey]
	if !ok {
		log.Debug().Msgf("There was no %s label found requiring action label handling", ActionLabelTimeKey)
		return nil
	}
	timeValNum, err := strconv.ParseInt(timeVal, 10, 64)
	if err != nil {
		return fmt.Errorf("Cannot convert unix time: %w", err)
	}
	secondsSinceLabel := time.Now().Unix() - timeValNum
	switch actionVal := k8sNode.Labels[ActionLabelKey]; actionVal {
	case UncordonAfterRebootLabelVal:
		uptime, err := n.uptime()
		if err != nil {
			return err
		}
		if secondsSinceLabel < uptime {
			log.Debug().Msg("The system has not restarted yet.")
			return nil
		}
		err = n.Uncordon(nodeName)
		if err != nil {
			return fmt.Errorf("Unable to uncordon node: %w", err)
		}
		err = n.RemoveNTHLabels(nodeName)
		if err != nil {
			return err
		}

		err = n.RemoveNTHTaints(nodeName)
		if err != nil {
			return err
		}

		log.Info().Msgf("Successfully completed action %s.", UncordonAfterRebootLabelVal)
	default:
		log.Debug().Msg("There are no label actions to handle.")
	}
	return nil
}

// fetchKubernetesNode will send an http request to the k8s api server and return the corev1 model node
func (n Node) fetchKubernetesNode(nodeName string) (*corev1.Node, error) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		Spec:       corev1.NodeSpec{},
	}
	if n.nthConfig.DryRun {
		return node, nil
	}

	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname=": nodeName}}
	listOptions := metav1.ListOptions{LabelSelector: labels.Set(labelSelector.MatchLabels).String()}
	matchingNodes, err := n.drainHelper.Client.CoreV1().Nodes().List(context.TODO(), listOptions)
	if err != nil || len(matchingNodes.Items) == 0 {
		log.Err(err).Msgf("Error when trying to list Nodes w/ label, falling back to direct Get lookup of node")
		return n.drainHelper.Client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	}
	return &matchingNodes.Items[0], nil
}

func (n Node) fetchAllPods(nodeName string) (*corev1.PodList, error) {
	if n.nthConfig.DryRun {
		log.Info().Msgf("Would have retrieved running pod list on node %s, but dry-run flag was set", nodeName)
		return &corev1.PodList{}, nil
	}
	return n.drainHelper.Client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
}

func getDrainHelper(nthConfig config.Config) (*drain.Helper, error) {
	drainHelper := &drain.Helper{
		Ctx:                 context.TODO(),
		Client:              &kubernetes.Clientset{},
		Force:               true,
		GracePeriodSeconds:  nthConfig.PodTerminationGracePeriod,
		IgnoreAllDaemonSets: nthConfig.IgnoreDaemonSets,
		DeleteEmptyDirData:  nthConfig.DeleteLocalData,
		Timeout:             time.Duration(nthConfig.NodeTerminationGracePeriod) * time.Second,
		Out:                 log.Logger,
		ErrOut:              log.Logger,
	}

	if nthConfig.DryRun {
		return drainHelper, nil
	}

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		return nil, err
	}
	drainHelper.Client = clientset

	return drainHelper, nil
}

func jsonPatchEscape(value string) string {
	value = strings.Replace(value, "~", "~0", -1)
	return strings.Replace(value, "/", "~1", -1)
}

func addTaint(node *corev1.Node, nth Node, taintKey string, taintValue string, effect corev1.TaintEffect) error {
	if nth.nthConfig.DryRun {
		log.Info().Msgf("Would have added taint (%s=%s:%s) to node %s, but dry-run flag was set", taintKey, taintValue, effect, nth.nthConfig.NodeName)
		return nil
	}

	retryDeadline := time.Now().Add(maxRetryDeadline)
	freshNode := node.DeepCopy()
	client := nth.drainHelper.Client
	var err error
	refresh := false
	for {
		if refresh {
			// Get the newest version of the node.
			freshNode, err = client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
			if err != nil || freshNode == nil {
				nodeErr := fmt.Errorf("failed to get node %v: %w", node.Name, err)
				log.Err(nodeErr).
					Str("taint_key", taintKey).
					Str("node_name", node.Name).
					Msg("Error while adding taint on node")
				return nodeErr
			}
		}

		if !addTaintToSpec(freshNode, taintKey, taintValue, effect) {
			if !refresh {
				// Make sure we have the latest version before skipping update.
				refresh = true
				continue
			}
			return nil
		}
		_, err = client.CoreV1().Nodes().Update(context.TODO(), freshNode, metav1.UpdateOptions{})
		if err != nil && errors.IsConflict(err) && time.Now().Before(retryDeadline) {
			refresh = true
			time.Sleep(conflictRetryInterval)
			continue
		}

		if err != nil {
			log.Err(err).
				Str("taint_key", taintKey).
				Str("node_name", node.Name).
				Msg("Error while adding taint on node")
			return err
		}
		log.Warn().
			Str("taint_key", taintKey).
			Str("node_name", node.Name).
			Msg("Successfully added taint on node")
		return nil
	}
}

func addTaintToSpec(node *corev1.Node, taintKey string, taintValue string, effect corev1.TaintEffect) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey {
			log.Debug().
				Str("taint_key", taintKey).
				Interface("taint", taint).
				Str("node_name", node.Name).
				Msg("Taint key already present on node")
			return false
		}
	}
	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
		Key:    taintKey,
		Value:  taintValue,
		Effect: effect,
	})
	return true
}

func removeTaint(node *corev1.Node, client kubernetes.Interface, taintKey string) (bool, error) {
	retryDeadline := time.Now().Add(maxRetryDeadline)
	freshNode := node.DeepCopy()
	var err error
	refresh := false
	for {
		if refresh {
			// Get the newest version of the node.
			freshNode, err = client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
			if err != nil || freshNode == nil {
				return false, fmt.Errorf("failed to get node %v: %v", node.Name, err)
			}
		}
		newTaints := make([]corev1.Taint, 0)
		for _, taint := range freshNode.Spec.Taints {
			if taint.Key == taintKey {
				log.Info().
					Interface("taint", taint).
					Str("node_name", node.Name).
					Msg("Releasing taint on node")
			} else {
				newTaints = append(newTaints, taint)
			}
		}
		if len(newTaints) == len(freshNode.Spec.Taints) {
			if !refresh {
				// Make sure we have the latest version before skipping update.
				refresh = true
				continue
			}
			return false, nil
		}

		freshNode.Spec.Taints = newTaints
		_, err = client.CoreV1().Nodes().Update(context.TODO(), freshNode, metav1.UpdateOptions{})

		if err != nil && errors.IsConflict(err) && time.Now().Before(retryDeadline) {
			refresh = true
			time.Sleep(conflictRetryInterval)
			continue
		}

		if err != nil {
			log.Err(err).
				Str("taint_key", taintKey).
				Str("node_name", node.Name).
				Msg("Error while releasing taint on node")
			return false, err
		}
		log.Info().
			Str("taint_key", taintKey).
			Str("node_name", node.Name).
			Msg("Successfully released taint on node")
		return true, nil
	}
}

func getUptimeFunc(uptimeFile string) uptime.UptimeFuncType {
	if uptimeFile != "" {
		return func() (int64, error) {
			return uptime.UptimeFromFile(uptimeFile)
		}
	}
	return uptime.Uptime
}
