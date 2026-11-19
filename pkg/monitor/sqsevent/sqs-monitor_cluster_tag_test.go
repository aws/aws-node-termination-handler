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

package sqsevent_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	testClusterTagKey = "example.com/cluster"
	testClusterName   = "my-cluster"
)

type deleteRecordingSQS struct {
	sqsiface.SQSAPI
	messages []*sqs.Message
	deleted  int
}

func (m *deleteRecordingSQS) ReceiveMessage(*sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{Messages: m.messages}, nil
}

func (m *deleteRecordingSQS) DeleteMessage(*sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error) {
	m.deleted++
	return &sqs.DeleteMessageOutput{}, nil
}

func describeInstancesWithClusterTag(withTag bool, value string) ec2.DescribeInstancesOutput {
	resp := getDescribeInstancesResp("ip-10-0-0-157.us-east-2.compute.internal", true, true)
	if withTag {
		instance := resp.Reservations[0].Instances[0]
		instance.Tags = append(instance.Tags, &ec2.Tag{Key: aws.String(testClusterTagKey), Value: aws.String(value)})
	}
	return resp
}

// runClusterTagMonitor processes one message per event and returns the events sent for draining, the number of deleted messages and the log output
func runClusterTagMonitor(t *testing.T, isolation string, describeResp ec2.DescribeInstancesOutput) ([]monitor.InterruptionEvent, int, string) {
	var logBuf bytes.Buffer
	origLogger := log.Logger
	log.Logger = zerolog.New(&logBuf).Level(zerolog.InfoLevel)
	defer func() { log.Logger = origLogger }()

	var drained []monitor.InterruptionEvent
	deleted := 0
	for _, event := range []sqsevent.EventBridgeEvent{spotItnEvent, asgLifecycleEvent, rebalanceRecommendationEvent} {
		msg, err := getSQSMessageFromEvent(event)
		h.Ok(t, err)
		sqsMock := &deleteRecordingSQS{messages: []*sqs.Message{&msg}}
		drainChan := make(chan monitor.InterruptionEvent, 1)
		sqsMonitor := sqsevent.SQSMonitor{
			SQS:                 sqsMock,
			EC2:                 h.MockedEC2{DescribeInstancesResp: describeResp},
			ASG:                 &h.MockedASG{},
			ManagedTag:          "aws-node-termination-handler/managed",
			CheckIfManaged:      true,
			QueueURL:            "https://test-queue",
			InterruptionChan:    drainChan,
			ClusterTagIsolation: isolation,
			ClusterTagKey:       testClusterTagKey,
			ClusterName:         testClusterName,
		}
		h.Ok(t, sqsMonitor.Monitor())
		select {
		case result := <-drainChan:
			drained = append(drained, result)
		default:
		}
		deleted += sqsMock.deleted
	}
	return drained, deleted, logBuf.String()
}

func TestMonitor_ClusterTagIsolationEnforceInCluster(t *testing.T) {
	drained, deleted, _ := runClusterTagMonitor(t, config.ClusterTagIsolationEnforce, describeInstancesWithClusterTag(true, testClusterName))
	h.Equals(t, 3, len(drained))
	h.Equals(t, 0, deleted)
}

func TestMonitor_ClusterTagIsolationEnforceOtherCluster(t *testing.T) {
	drained, deleted, logs := runClusterTagMonitor(t, config.ClusterTagIsolationEnforce, describeInstancesWithClusterTag(true, "other-cluster"))
	h.Equals(t, 0, len(drained))
	h.Equals(t, 3, deleted)
	assertDropLog(t, logs, 3, "other-cluster")
}

func TestMonitor_ClusterTagIsolationEnforceUntagged(t *testing.T) {
	drained, deleted, logs := runClusterTagMonitor(t, config.ClusterTagIsolationEnforce, describeInstancesWithClusterTag(false, ""))
	h.Equals(t, 0, len(drained))
	h.Equals(t, 3, deleted)
	assertDropLog(t, logs, 3, "")
}

func TestMonitor_ClusterTagIsolationDisableIgnoresClusterTag(t *testing.T) {
	for _, resp := range []ec2.DescribeInstancesOutput{
		describeInstancesWithClusterTag(true, "other-cluster"),
		describeInstancesWithClusterTag(false, ""),
	} {
		drained, deleted, logs := runClusterTagMonitor(t, config.ClusterTagIsolationDisable, resp)
		h.Equals(t, 3, len(drained))
		h.Equals(t, 0, deleted)
		h.Assert(t, !strings.Contains(logs, "not tagged for this cluster"), "isolation disable should not drop events: %s", logs)
	}
}

func TestMonitor_ClusterTagIsolationEnforceWithoutManagedTagCheck(t *testing.T) {
	msg, err := getSQSMessageFromEvent(spotItnEvent)
	h.Ok(t, err)
	sqsMock := &deleteRecordingSQS{messages: []*sqs.Message{&msg}}
	drainChan := make(chan monitor.InterruptionEvent, 1)
	sqsMonitor := sqsevent.SQSMonitor{
		SQS:                 sqsMock,
		EC2:                 h.MockedEC2{DescribeInstancesResp: describeInstancesWithClusterTag(true, "other-cluster")},
		CheckIfManaged:      false,
		QueueURL:            "https://test-queue",
		InterruptionChan:    drainChan,
		ClusterTagIsolation: config.ClusterTagIsolationEnforce,
		ClusterTagKey:       testClusterTagKey,
		ClusterName:         testClusterName,
	}
	h.Ok(t, sqsMonitor.Monitor())
	select {
	case <-drainChan:
		t.Fatal("expected no event for an instance tagged for another cluster")
	default:
	}
	h.Equals(t, 1, sqsMock.deleted)
}

func assertDropLog(t *testing.T, logs string, count int, value string) {
	found := 0
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		entry := map[string]interface{}{}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry["message"] != "ignoring resource not tagged for this cluster" {
			continue
		}
		found++
		h.Equals(t, "info", entry["level"])
		instanceID, _ := entry["resource"].(string)
		h.Assert(t, strings.HasPrefix(instanceID, "i-"), "resource field missing: %v", entry)
		h.Equals(t, testClusterTagKey, entry["tagKey"])
		h.Equals(t, value, entry["tagValue"])
		h.Equals(t, testClusterName, entry["clusterName"])
		h.Assert(t, len(entry) == 6, "fields should be level, message, resource, tagKey, clusterName, tagValue: %v", entry)
	}
	h.Equals(t, count, found)
}
