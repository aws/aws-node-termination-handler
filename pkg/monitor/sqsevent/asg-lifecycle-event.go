// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package sqsevent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

/* Example SQS ASG Lifecycle Termination Event Message:
{
  "version": "0",
  "id": "782d5b4c-0f6f-1fd6-9d62-ecf6aed0a470",
  "detail-type": "EC2 Instance-terminate Lifecycle Action",
  "source": "aws.autoscaling",
  "account": "123456789012",
  "time": "2020-07-01T22:19:58Z",
  "region": "us-east-1",
  "resources": [
    "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:26e7234b-03a4-47fb-b0a9-2b241662774e:autoScalingGroupName/testt1.demo-0a20f32c.kops.sh"
  ],
  "detail": {
    "LifecycleActionToken": "0befcbdb-6ecd-498a-9ff7-ae9b54447cd6",
    "AutoScalingGroupName": "testt1.demo-0a20f32c.kops.sh",
    "LifecycleHookName": "cluster-termination-handler",
    "EC2InstanceId": "i-0633ac2b0d9769723",
    "LifecycleTransition": "autoscaling:EC2_INSTANCE_TERMINATING"
  }
}
*/

const TEST_NOTIFICATION = "autoscaling:TEST_NOTIFICATION"

// LifecycleDetail provides the ASG lifecycle event details
type LifecycleDetail struct {
	LifecycleActionToken string `json:"LifecycleActionToken"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	LifecycleHookName    string `json:"LifecycleHookName"`
	EC2InstanceID        string `json:"EC2InstanceId"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	Event                string `json:"Event"`
	RequestID            string `json:"RequestId"`
	Time                 string `json:"Time"`
}

func (m SQSMonitor) asgTerminationToInterruptionEvent(event *EventBridgeEvent, message *sqs.Message) (*monitor.InterruptionEvent, error) {
	lifecycleDetail := &LifecycleDetail{}
	err := json.Unmarshal(event.Detail, lifecycleDetail)
	if err != nil {
		return nil, err
	}

	if lifecycleDetail.Event == TEST_NOTIFICATION || lifecycleDetail.LifecycleTransition == TEST_NOTIFICATION {
		return nil, skip{fmt.Errorf("message is an ASG test notification")}
	}

	nodeInfo, err := m.getNodeInfo(lifecycleDetail.EC2InstanceID)
	if err != nil {
		return nil, err
	}

	interruptionEvent := monitor.InterruptionEvent{
		EventID:              fmt.Sprintf("asg-lifecycle-term-%x", event.ID),
		Kind:                 monitor.ASGLifecycleKind,
		Monitor:              SQSMonitorKind,
		AutoScalingGroupName: lifecycleDetail.AutoScalingGroupName,
		StartTime:            event.getTime(),
		NodeName:             nodeInfo.Name,
		IsManaged:            nodeInfo.IsManaged,
		InstanceID:           lifecycleDetail.EC2InstanceID,
		ProviderID:           nodeInfo.ProviderID,
		Description:          fmt.Sprintf("ASG Lifecycle Termination event received. Instance will be interrupted at %s \n", event.getTime()),
	}

	interruptionEvent.PostDrainTask = func(interruptionEvent monitor.InterruptionEvent, _ node.Node) error {
		_, err := m.continueLifecycleAction(lifecycleDetail)
		if err != nil {
			if aerr, ok := err.(awserr.RequestFailure); ok && aerr.StatusCode() != 400 {
				return err
			}
		}
		log.Info().Msgf("Completed ASG Lifecycle Hook (%s) for instance %s",
			lifecycleDetail.LifecycleHookName,
			lifecycleDetail.EC2InstanceID)
		errs := m.deleteMessages([]*sqs.Message{message})
		if errs != nil {
			return errs[0]
		}
		return nil
	}

	interruptionEvent.PreDrainTask = func(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
		err := n.TaintASGLifecycleTermination(interruptionEvent.NodeName, interruptionEvent.EventID)
		if err != nil {
			log.Err(err).Msgf("Unable to taint node with taint %s:%s", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID)
		}
		return nil
	}

	return &interruptionEvent, nil
}

// Continues the lifecycle hook thereby indicating a successful action occured
func (m SQSMonitor) continueLifecycleAction(lifecycleDetail *LifecycleDetail) (*autoscaling.CompleteLifecycleActionOutput, error) {
	return m.completeLifecycleAction(&autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  &lifecycleDetail.AutoScalingGroupName,
		LifecycleActionResult: aws.String("CONTINUE"),
		LifecycleHookName:     &lifecycleDetail.LifecycleHookName,
		LifecycleActionToken:  &lifecycleDetail.LifecycleActionToken,
		InstanceId:            &lifecycleDetail.EC2InstanceID,
	})
}

// Completes the ASG launch lifecycle hook if the new EC2 instance launched by ASG is Ready in the cluster
func (m SQSMonitor) asgCompleteLaunchLifecycle(event *EventBridgeEvent) error {
	lifecycleDetail := &LifecycleDetail{}
	err := json.Unmarshal(event.Detail, lifecycleDetail)
	if err != nil {
		return fmt.Errorf("unmarshing ASG lifecycle event: %w", err)
	}

	if lifecycleDetail.Event == TEST_NOTIFICATION || lifecycleDetail.LifecycleTransition == TEST_NOTIFICATION {
		return skip{fmt.Errorf("message is an ASG test notification")}
	}

	if isNodeReady(lifecycleDetail) {
		_, err = m.continueLifecycleAction(lifecycleDetail)
	} else {
		err = skip{fmt.Errorf("new ASG instance has not connected to cluster")}
	}
	return err
}

// If the Node, new EC2 instance, is ready in the K8s cluster
func isNodeReady(lifecycleDetail *LifecycleDetail) bool {
	nodes, err := getNodes()
	if err != nil {
		log.Err(fmt.Errorf("getting nodes from cluster: %w", err))
		return false
	}

	for _, node := range nodes.Items {
		instanceID := getInstanceID(node)
		if instanceID != lifecycleDetail.EC2InstanceID {
			continue
		}

		conditions := node.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				return true
			}
		}
		log.Error().Msg(fmt.Sprintf("ec2 instance, %s, found, but not ready in cluster", instanceID))
	}
	log.Error().Msg(fmt.Sprintf("ec2 instance, %s, not found in cluster", lifecycleDetail.EC2InstanceID))
	return false
}

// Gets Nodes connected to K8s cluster
func getNodes() (*v1.NodeList, error) {
	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("retreiving cluster config: %w", err)
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		return nil, fmt.Errorf("creating new clientset with config: %w", err)
	}
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("retreiving nodes from cluster: %w", err)
	}
	return nodes, err
}

// Gets EC2 InstanceID from ProviderID, format: aws:///$az/$instanceid
func getInstanceID(node v1.Node) string {
	providerID := node.Spec.ProviderID
	providerIDSplit := strings.Split(providerID, "/")
	instanceID := providerIDSplit[len(providerID)-1]
	return instanceID
}
