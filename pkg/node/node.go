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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	maxTaintValueLength = 63
)

var (
	maxRetryDeadline      time.Duration = 5 * time.Second
	conflictRetryInterval time.Duration = 750 * time.Millisecond
)

var uptimeFile = "/proc/uptime"

// Node represents a kubernetes node with functions to manipulate its state via the kubernetes api server
type Node struct {
	nthConfig   config.Config
	drainHelper *drain.Helper
}

// New will construct a node struct to perform various node function through the kubernetes api server
func New(nthConfig config.Config) (*Node, error) {
	drainHelper, err := getDrainHelper(nthConfig)
	if err != nil {
		return nil, err
	}
	return &Node{
		nthConfig:   nthConfig,
		drainHelper: drainHelper,
	}, nil
}

// NewWithValues will construct a node struct with a drain helper
func NewWithValues(nthConfig config.Config, drainHelper *drain.Helper) (*Node, error) {
	return &Node{
		nthConfig:   nthConfig,
		drainHelper: drainHelper,
	}, nil
}

// CordonAndDrain will cordon the node and evict pods based on the config
func (n Node) CordonAndDrain() error {
	if n.nthConfig.DryRun {
		log.Log().Msgf("Node %s would have been cordoned and drained, but dry-run flag was set", n.nthConfig.NodeName)
		return nil
	}
	err := n.Cordon()
	if err != nil {
		return err
	}
	// Delete all pods on the node
	err = drain.RunNodeDrain(n.drainHelper, n.nthConfig.NodeName)
	if err != nil {
		return err
	}
	return nil
}

// Cordon will add a NoSchedule on the node
func (n Node) Cordon() error {
	if n.nthConfig.DryRun {
		log.Log().Msgf("Node %s would have been cordoned, but dry-run flag was set", n.nthConfig.NodeName)
		return nil
	}
	node, err := n.fetchKubernetesNode()
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
func (n Node) Uncordon() error {
	if n.nthConfig.DryRun {
		log.Log().Msgf("Node %s would have been uncordoned, but dry-run flag was set", n.nthConfig.NodeName)
		return nil
	}
	node, err := n.fetchKubernetesNode()
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
func (n Node) IsUnschedulable() (bool, error) {
	if n.nthConfig.DryRun {
		log.Log().Msg("IsUnschedulable returning false since dry-run is set")
		return false, nil
	}
	node, err := n.fetchKubernetesNode()
	if err != nil {
		return true, err
	}
	return node.Spec.Unschedulable, nil
}

// MarkWithEventID will add the drain event ID to the node to be properly ignored after a system restart event
func (n Node) MarkWithEventID(eventID string) error {
	err := n.addLabel(EventIDLabelKey, eventID)
	if err != nil {
		return fmt.Errorf("Unable to label node with event ID %s=%s: %w", EventIDLabelKey, eventID, err)
	}
	return nil
}

// RemoveNTHLabels will remove all the custom NTH labels added to the node
func (n Node) RemoveNTHLabels() error {
	for _, label := range []string{EventIDLabelKey, ActionLabelKey, ActionLabelTimeKey} {
		err := n.removeLabel(label)
		if err != nil {
			return fmt.Errorf("Unable to remove %s from node: %w", label, err)
		}
	}
	return nil
}

// GetEventID will retrieve the event ID value from the node label
func (n Node) GetEventID() (string, error) {
	node, err := n.fetchKubernetesNode()
	if err != nil {
		return "", fmt.Errorf("Could not get event ID label from node: %w", err)
	}
	val, ok := node.Labels[EventIDLabelKey]
	if n.nthConfig.DryRun && !ok {
		log.Log().Msgf("Would have returned Error: Event ID Lable %s was not found on the node, but dry-run flag was set", EventIDLabelKey)
		return "", nil
	}
	if !ok {
		return "", fmt.Errorf("Event ID Label %s was not found on the node", EventIDLabelKey)
	}
	return val, nil
}

// MarkForUncordonAfterReboot adds labels to the kubernetes node which NTH will read upon reboot
func (n Node) MarkForUncordonAfterReboot() error {
	// adds label to node so that the system will uncordon the node after the scheduled reboot has taken place
	err := n.addLabel(ActionLabelKey, UncordonAfterRebootLabelVal)
	if err != nil {
		return fmt.Errorf("Unable to label node with action to uncordon after system-reboot: %w", err)
	}
	// adds label with the current time which is checked against the uptime of the node when processing labels on startup
	err = n.addLabel(ActionLabelTimeKey, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		// if time can't be recorded, rollback the action label
		n.removeLabel(ActionLabelKey)
		return fmt.Errorf("Unable to label node with action time for uncordon after system-reboot: %w", err)
	}
	return nil
}

// addLabel will add a label to the node given a label key and value
func (n Node) addLabel(key string, value string) error {
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
	node, err := n.fetchKubernetesNode()
	if err != nil {
		return err
	}
	if n.nthConfig.DryRun {
		log.Log().Msgf("Would have added label (%s=%s) to node %s, but dry-run flag was set", key, value, n.nthConfig.NodeName)
		return nil
	}
	_, err = n.drainHelper.Client.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, payloadBytes)
	if err != nil {
		return fmt.Errorf("%v node Patch failed when adding a label to the node: %w", node.Name, err)
	}
	return nil
}

// removeLabel will remove a node label given a label key
func (n Node) removeLabel(key string) error {
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
	node, err := n.fetchKubernetesNode()
	if err != nil {
		return err
	}
	if n.nthConfig.DryRun {
		log.Log().Msgf("Would have removed label with key %s from node %s, but dry-run flag was set", key, n.nthConfig.NodeName)
		return nil
	}
	_, err = n.drainHelper.Client.CoreV1().Nodes().Patch(node.Name, types.JSONPatchType, payload)
	if err != nil {
		return fmt.Errorf("%v node Patch failed when removing a label from the node: %w", node.Name, err)
	}
	return nil
}

// TaintSpotItn adds the spot termination notice taint onto a node
func (n Node) TaintSpotItn(eventID string) error {
	k8sNode, err := n.fetchKubernetesNode()
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n.drainHelper.Client, SpotInterruptionTaint, eventID, corev1.TaintEffectNoSchedule)
}

// TaintScheduledMaintenance adds the scheduled maintenance taint onto a node
func (n Node) TaintScheduledMaintenance(eventID string) error {
	k8sNode, err := n.fetchKubernetesNode()
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	if len(eventID) > 63 {
		eventID = eventID[:maxTaintValueLength]
	}

	return addTaint(k8sNode, n.drainHelper.Client, ScheduledMaintenanceTaint, eventID, corev1.TaintEffectNoSchedule)
}

// CleanAllTaints removes NTH-specific taints from a node
func (n Node) CleanAllTaints() error {
	k8sNode, err := n.fetchKubernetesNode()
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}

	taints := []string{SpotInterruptionTaint, ScheduledMaintenanceTaint}

	for _, taint := range taints {
		_, err = cleanTaint(k8sNode, n.drainHelper.Client, taint)
		if err != nil {
			return fmt.Errorf("Unable to clean taint %s from node %s", taint, n.nthConfig.NodeName)
		}
	}

	return nil
}

// IsLabeledWithAction will return true if the current node is labeled with NTH action labels
func (n Node) IsLabeledWithAction() (bool, error) {
	k8sNode, err := n.fetchKubernetesNode()
	if err != nil {
		return false, fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}
	_, actionLabelOK := k8sNode.Labels[ActionLabelKey]
	_, eventIDLabelOK := k8sNode.Labels[EventIDLabelKey]
	return actionLabelOK && eventIDLabelOK, nil
}

// UncordonIfRebooted will check for node labels to trigger an uncordon because of a system-reboot scheduled event
func (n Node) UncordonIfRebooted() error {
	k8sNode, err := n.fetchKubernetesNode()
	if err != nil {
		return fmt.Errorf("Unable to fetch kubernetes node from API: %w", err)
	}
	timeVal, ok := k8sNode.Labels[ActionLabelTimeKey]
	if !ok {
		log.Log().Msgf("There was no %s label found requiring action label handling", ActionLabelTimeKey)
		return nil
	}
	timeValNum, err := strconv.ParseInt(timeVal, 10, 64)
	if err != nil {
		return fmt.Errorf("Cannot convert unix time: %w", err)
	}
	secondsSinceLabel := time.Now().Unix() - timeValNum
	switch actionVal := k8sNode.Labels[ActionLabelKey]; actionVal {
	case UncordonAfterRebootLabelVal:
		uptime, err := getSystemUptime(uptimeFile)
		if err != nil {
			return err
		}
		if secondsSinceLabel < int64(uptime) {
			log.Log().Msg("The system has not restarted yet.")
			return nil
		}
		err = n.Uncordon()
		if err != nil {
			return fmt.Errorf("Unable to uncordon node: %w", err)
		}
		err = n.RemoveNTHLabels()
		if err != nil {
			return err
		}

		err = n.CleanAllTaints()
		if err != nil {
			return err
		}

		log.Log().Msgf("Successfully completed action %s.", UncordonAfterRebootLabelVal)
	default:
		log.Log().Msg("There are no label actions to handle.")
	}
	return nil
}

// fetchKubernetesNode will send an http request to the k8s api server and return the corev1 model node
func (n Node) fetchKubernetesNode() (*corev1.Node, error) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: n.nthConfig.NodeName},
	}
	if n.nthConfig.DryRun {
		return node, nil
	}
	fmt.Println(n.nthConfig.NodeName)
	return n.drainHelper.Client.CoreV1().Nodes().Get(n.nthConfig.NodeName, metav1.GetOptions{})
}

func getDrainHelper(nthConfig config.Config) (*drain.Helper, error) {
	drainHelper := &drain.Helper{
		Client:              &kubernetes.Clientset{},
		Force:               true,
		GracePeriodSeconds:  nthConfig.PodTerminationGracePeriod,
		IgnoreAllDaemonSets: nthConfig.IgnoreDaemonSets,
		DeleteLocalData:     nthConfig.DeleteLocalData,
		Timeout:             time.Duration(nthConfig.NodeTerminationGracePeriod) * time.Second,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
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

func getSystemUptime(filename string) (float64, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return 0, fmt.Errorf("Not able to read %s: %w", filename, err)
	}

	uptime, err := strconv.ParseFloat(strings.Split(string(data), " ")[0], 64)
	if err != nil {
		return 0, fmt.Errorf("Not able to parse %s to Float64: %w", filename, err)
	}
	return uptime, nil
}

func jsonPatchEscape(value string) string {
	value = strings.Replace(value, "~", "~0", -1)
	return strings.Replace(value, "/", "~1", -1)
}

func addTaint(node *corev1.Node, client kubernetes.Interface, taintKey string, taintValue string, effect corev1.TaintEffect) error {
	retryDeadline := time.Now().Add(maxRetryDeadline)
	freshNode := node.DeepCopy()
	var err error
	refresh := false
	for {
		if refresh {
			// Get the newest version of the node.
			freshNode, err = client.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
			if err != nil || freshNode == nil {
				log.Log().Msgf("Error while adding %v taint on node %v: %v", taintKey, node.Name, err)
				return fmt.Errorf("failed to get node %v: %v", node.Name, err)
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
		_, err = client.CoreV1().Nodes().Update(freshNode)
		if err != nil && errors.IsConflict(err) && time.Now().Before(retryDeadline) {
			refresh = true
			time.Sleep(conflictRetryInterval)
			continue
		}

		if err != nil {
			log.Log().Msgf("Error while adding %v taint on node %v: %v", taintKey, node.Name, err)
			return err
		}
		log.Log().Msgf("Successfully added %v on node %v", taintKey, node.Name)
		return nil
	}
}

func addTaintToSpec(node *corev1.Node, taintKey string, taintValue string, effect corev1.TaintEffect) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey {
			log.Log().Msgf("%v already present on node %v, taint: %v", taintKey, node.Name, taint)
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

func cleanTaint(node *corev1.Node, client kubernetes.Interface, taintKey string) (bool, error) {
	retryDeadline := time.Now().Add(maxRetryDeadline)
	freshNode := node.DeepCopy()
	var err error
	refresh := false
	for {
		if refresh {
			// Get the newest version of the node.
			freshNode, err = client.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
			if err != nil || freshNode == nil {
				log.Log().Msgf("Error while adding %v taint on node %v: %v", taintKey, node.Name, err)
				return false, fmt.Errorf("failed to get node %v: %v", node.Name, err)
			}
		}
		newTaints := make([]corev1.Taint, 0)
		for _, taint := range freshNode.Spec.Taints {
			if taint.Key == taintKey {
				log.Log().Msgf("Releasing taint %+v on node %v", taint, node.Name)
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
		_, err = client.CoreV1().Nodes().Update(freshNode)

		if err != nil && errors.IsConflict(err) && time.Now().Before(retryDeadline) {
			refresh = true
			time.Sleep(conflictRetryInterval)
			continue
		}

		if err != nil {
			log.Log().Msgf("Error while releasing %v taint on node %v: %v", taintKey, node.Name, err)
			return false, err
		}
		log.Log().Msgf("Successfully released %v on node %v", taintKey, node.Name)
		return true, nil
	}
}
