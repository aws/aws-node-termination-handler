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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	api "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubectl/pkg/drain"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2helper"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/uptime"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	mockNth         = "aws.node.termination.handler"
	mockErrorEvent  = "mockErrorEvent"
	mockAction      = "cordon-and-drain"
	mockNodeName1   = "nodeName1"
	mockNodeName2   = "nodeName2"
	mockNodeName3   = "nodeName3"
	mockEventID1    = "eventID1"
	mockEventID2    = "eventID2"
	mockEventID3    = "eventID3"
	successStatus   = "success"
	errorStatus     = "error"
	mockDefaultPort = 9092
	mockClosedPort  = 9093
	instanceId1     = "i-1"
	instanceId2     = "i-2"
	instanceId3     = "i-3"
)

func TestInitMetrics(t *testing.T) {
	getMetrics(t)

	responseRecorder := mockMetricsRequest()

	validateStatus(t, responseRecorder)

	metricsMap := getMetricsMap(responseRecorder.Body.String())

	runtimeMetrics := []string{
		"go_gc_gogc_percent",
		"go_memstats_frees_total",
		"go_goroutines",
	}

	for _, metricName := range runtimeMetrics {
		_, exists := metricsMap[metricName]
		h.Assert(t, exists, fmt.Sprintf("%v metric should be present", metricName))
	}
}

func TestErrorEventsInc(t *testing.T) {
	metrics := getMetrics(t)

	metrics.ErrorEventsInc(mockErrorEvent)

	responseRecorder := mockMetricsRequest()

	validateStatus(t, responseRecorder)

	metricsMap := getMetricsMap(responseRecorder.Body.String())

	validateEventErrorTotal(t, metricsMap, 1)
	validateActionTotalV2(t, metricsMap, 0, successStatus)
	validateActionTotalV2(t, metricsMap, 0, errorStatus)
}

func TestNodeActionsInc(t *testing.T) {
	metrics := getMetrics(t)

	metrics.NodeActionsInc(mockAction, mockNodeName1, mockEventID1, nil)
	metrics.NodeActionsInc(mockAction, mockNodeName2, mockEventID2, nil)
	metrics.NodeActionsInc(mockAction, mockNodeName3, mockEventID3, errors.New("mockError"))

	responseRecorder := mockMetricsRequest()

	validateStatus(t, responseRecorder)

	metricsMap := getMetricsMap(responseRecorder.Body.String())

	validateEventErrorTotal(t, metricsMap, 0)
	validateActionTotalV2(t, metricsMap, 2, successStatus)
	validateActionTotalV2(t, metricsMap, 1, errorStatus)
}

func TestRegisterMetricsWith(t *testing.T) {
	const errorEventMetricsTotal = 23
	const successActionMetricsTotal = 31
	const errorActionMetricsTotal = 97
	const managedInstancesTotal = 3
	const managedNodesTotal = 5

	metrics := getMetrics(t)

	errorEventlabels := []attribute.KeyValue{labelEventErrorWhereKey.String(mockErrorEvent)}
	successActionlabels := []attribute.KeyValue{labelNodeActionKey.String(mockAction), labelNodeStatusKey.String(successStatus)}
	errorActionlabels := []attribute.KeyValue{labelNodeActionKey.String(mockAction), labelNodeStatusKey.String(errorStatus)}

	for i := 0; i < errorEventMetricsTotal; i++ {
		metrics.errorEventsCounter.Add(context.Background(), 1, api.WithAttributes(errorEventlabels...))
	}
	for i := 0; i < successActionMetricsTotal; i++ {
		metrics.actionsCounterV2.Add(context.Background(), 1, api.WithAttributes(successActionlabels...))
	}
	for i := 0; i < errorActionMetricsTotal; i++ {
		metrics.actionsCounterV2.Add(context.Background(), 1, api.WithAttributes(errorActionlabels...))
	}

	metrics.NodesRecord(managedNodesTotal)
	metrics.InstancesRecord(managedInstancesTotal)

	responseRecorder := mockMetricsRequest()

	validateStatus(t, responseRecorder)

	metricsMap := getMetricsMap(responseRecorder.Body.String())

	validateEventErrorTotal(t, metricsMap, errorEventMetricsTotal)
	validateActionTotalV2(t, metricsMap, successActionMetricsTotal, successStatus)
	validateActionTotalV2(t, metricsMap, errorActionMetricsTotal, errorStatus)
	validateGauge(t, metricsMap, managedNodesTotal, "nth_tagged_nodes")
	validateGauge(t, metricsMap, managedInstancesTotal, "nth_tagged_instances")
}

func TestServeNodeMetrics(t *testing.T) {
	metrics := getMetrics(t)
	metrics.ec2Helper = ec2helper.New(h.MockedEC2{
		DescribeInstancesResp: ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: []*ec2.Instance{
						{
							InstanceId: aws.String(instanceId1),
						},
						{
							InstanceId: aws.String(instanceId2),
						},
						{
							InstanceId: aws.String(instanceId3),
						},
					},
				},
			},
		},
	})

	helper := getDrainHelper(fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Spec:       v1.NodeSpec{ProviderID: fmt.Sprintf("aws:///us-west-2a/%s", instanceId1)},
		},
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
			Spec:       v1.NodeSpec{ProviderID: fmt.Sprintf("aws:///us-west-2a/%s", instanceId2)},
		},
	))

	node, err := node.NewWithValues(config.Config{}, helper, uptime.Uptime)
	h.Ok(t, err)

	metrics.node = node
	metrics.serveNodeMetrics()

	responseRecorder := mockMetricsRequest()

	validateStatus(t, responseRecorder)

	metricsMap := getMetricsMap(responseRecorder.Body.String())

	validateGauge(t, metricsMap, 2, "nth_tagged_nodes")
	validateGauge(t, metricsMap, 3, "nth_tagged_instances")
}

func TestServeMetrics(t *testing.T) {
	server := serveMetrics(mockDefaultPort)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown server: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", mockDefaultPort), time.Second)
	if err != nil {
		t.Errorf("server is not listening on port %d: %v", mockDefaultPort, err)
	}
	conn.Close()

	conn, err = net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", mockClosedPort), time.Second)
	if err == nil {
		conn.Close()
		t.Errorf("server should not be listening on port %d: %v", mockClosedPort, err)
	}
}

func getMetrics(t *testing.T) *Metrics {
	exporter, err := prometheus.New()
	if err != nil {
		t.Errorf("failed to create Prometheus exporter: %v", err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	metrics, err := registerMetricsWith(provider)
	if err != nil {
		t.Errorf("failed to register metrics with Prometheus provider: %v", err)
	}
	metrics.enabled = true

	t.Cleanup(func() {
		if provider != nil {
			if err := provider.Shutdown(context.Background()); err != nil {
				t.Errorf("failed to shutdown provider: %v", err)
			}
		}
	})

	return &metrics
}

func mockMetricsRequest() *httptest.ResponseRecorder {
	handler := promhttp.Handler()
	req := httptest.NewRequest("GET", metricsEndpoint, nil)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, req)
	return responseRecorder
}

func validateStatus(t *testing.T, responseRecorder *httptest.ResponseRecorder) {
	status := responseRecorder.Code
	h.Equals(t, http.StatusOK, status)
}

// This method take response body got from Prometheus exporter as arg
// Example:
// # HELP go_goroutines Number of goroutines that currently exist.
// # TYPE go_goroutines gauge
// go_goroutines 6
func getMetricsMap(body string) map[string]string {
	metricsMap := make(map[string]string)
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		metricsMap[key] = value
	}
	return metricsMap
}

func getDrainHelper(client *fake.Clientset) *drain.Helper {
	return &drain.Helper{
		Ctx:                 context.TODO(),
		Client:              client,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             time.Duration(120) * time.Second,
		Out:                 log.Logger,
		ErrOut:              log.Logger,
	}
}

func validateEventErrorTotal(t *testing.T, metricsMap map[string]string, expectedTotal int) {
	eventErrorTotalKey := fmt.Sprintf("events_error_total{event_error_where=\"%v\",otel_scope_name=\"%v\",otel_scope_version=\"\"}", mockErrorEvent, mockNth)
	actualValue, exists := metricsMap[eventErrorTotalKey]
	if !exists {
		actualValue = "0"
	}
	h.Equals(t, strconv.Itoa(expectedTotal), actualValue)
}

func validateActionTotalV2(t *testing.T, metricsMap map[string]string, expectedTotal int, nodeStatus string) {
	actionTotalKey := fmt.Sprintf("actions_total{node_action=\"%v\",node_status=\"%v\",otel_scope_name=\"%v\",otel_scope_version=\"\"}", mockAction, nodeStatus, mockNth)
	actualValue, exists := metricsMap[actionTotalKey]
	if !exists {
		actualValue = "0"
	}
	h.Equals(t, strconv.Itoa(expectedTotal), actualValue)
}

func validateGauge(t *testing.T, metricsMap map[string]string, expectedTotal int, name string) {
	actionTotalKey := fmt.Sprintf("%v{otel_scope_name=\"%v\",otel_scope_version=\"\"}", name, mockNth)
	actualValue, exists := metricsMap[actionTotalKey]
	if !exists {
		actualValue = "0"
	}
	h.Equals(t, strconv.Itoa(expectedTotal), actualValue)
}
