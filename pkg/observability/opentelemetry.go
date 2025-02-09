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
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2helper"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	api "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
)

var (
	labelEventErrorWhereKey = attribute.Key("event/error/where")

	labelNodeActionKey = attribute.Key("node/action")
	labelNodeStatusKey = attribute.Key("node/status")
	labelNodeNameKey   = attribute.Key("node/name")
	labelEventIDKey    = attribute.Key("node/event-id")
	metricsEndpoint    = "/metrics"
)

// Metrics represents the stats for observability
type Metrics struct {
	enabled                 bool
	nthConfig               config.Config
	ec2Helper               ec2helper.EC2Helper
	node                    *node.Node
	meter                   api.Meter
	actionsCounter          api.Int64Counter
	actionsCounterV2        api.Int64Counter
	errorEventsCounter      api.Int64Counter
	nthTaggedNodesGauge     api.Int64Gauge
	nthTaggedInstancesGauge api.Int64Gauge
}

// InitMetrics will initialize, register and expose, via http server, the metrics with Opentelemetry.
func InitMetrics(enabled bool, port int) (Metrics, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	metrics, err := registerMetricsWith(provider)
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to register metrics with Prometheus provider: %w", err)
	}
	metrics.enabled = enabled

	// Starts an async process to collect golang runtime stats
	// go.opentelemetry.io/contrib/instrumentation/runtime
	err = runtime.Start(runtime.WithMeterProvider(provider), runtime.WithMinimumReadMemStatsInterval(1*time.Second))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to start Go runtime metrics collection: %w", err)
	}

	if enabled {
		serveMetrics(port)
	}

	return metrics, nil
}

func (m Metrics) InitNodeMetrics(nthConfig config.Config, node *node.Node, ec2 ec2iface.EC2API) {
	m.nthConfig = nthConfig
	m.ec2Helper = ec2helper.New(ec2)
	m.node = node

	// Run a periodic task
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.serveNodeMetrics()
	}
}

func (m Metrics) serveNodeMetrics() {
	instanceIdsMap, err := m.ec2Helper.GetInstanceIdsMapByTagKey(m.nthConfig.ManagedTag)
	if err != nil || instanceIdsMap == nil {
		log.Err(err).Msg("Failed to get AWS instance ids")
		return
	}

	m.InstancesRecord(int64(len(instanceIdsMap)))

	nodeInstanceIds, err := m.node.FetchKubernetesNodeInstanceIds()
	if err != nil || nodeInstanceIds == nil {
		log.Err(err).Msg("Failed to get node instance ids")
	} else {
		nodeCount := 0
		for _, id := range nodeInstanceIds {
			if _, ok := instanceIdsMap[id]; ok {
				nodeCount++
			}
		}
		m.NodesRecord(int64(nodeCount))
	}
}

// ErrorEventsInc will increment one for the event errors counter, partitioned by action, and only if metrics are enabled.
func (m Metrics) ErrorEventsInc(where string) {
	if !m.enabled {
		return
	}
	m.errorEventsCounter.Add(context.Background(), 1, api.WithAttributes(labelEventErrorWhereKey.String(where)))
}

// NodeActionsInc will increment one for the node stats counter, partitioned by action, nodeName and status, and only if metrics are enabled.
func (m Metrics) NodeActionsInc(action, nodeName string, eventID string, err error) {
	if !m.enabled {
		return
	}

	labels := []attribute.KeyValue{labelNodeActionKey.String(action), labelNodeNameKey.String(nodeName), labelEventIDKey.String(eventID)}
	labelsV2 := []attribute.KeyValue{labelNodeActionKey.String(action)}
	if err != nil {
		labels = append(labels, labelNodeStatusKey.String("error"))
		labelsV2 = append(labelsV2, labelNodeStatusKey.String("error"))
	} else {
		labels = append(labels, labelNodeStatusKey.String("success"))
		labelsV2 = append(labelsV2, labelNodeStatusKey.String("success"))
	}

	m.actionsCounter.Add(context.Background(), 1, api.WithAttributes(labels...))
	m.actionsCounterV2.Add(context.Background(), 1, api.WithAttributes(labelsV2...))
}

func (m Metrics) NodesRecord(num int64) {
	if !m.enabled {
		return
	}

	m.nthTaggedNodesGauge.Record(context.Background(), num)
}

func (m Metrics) InstancesRecord(num int64) {
	if !m.enabled {
		return
	}

	m.nthTaggedInstancesGauge.Record(context.Background(), num)
}

func registerMetricsWith(provider *metric.MeterProvider) (Metrics, error) {
	meter := provider.Meter("aws.node.termination.handler")

	// Deprecated: actionsCounter metric has a high label cardinality, resulting in numerous time-series which utilize
	// a large amount of memory. Use actionsCounterV2 metric instead.
	name := "actions.node"
	actionsCounter, err := meter.Int64Counter(name, api.WithDescription("Number of actions per node"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus counter %q: %w", name, err)
	}
	actionsCounter.Add(context.Background(), 0)

	// Recommended replacement for actionsCounter metric
	name = "actions"
	actionsCounterV2, err := meter.Int64Counter(name, api.WithDescription("Number of actions"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus counter %q: %w", name, err)
	}
	actionsCounterV2.Add(context.Background(), 0)

	name = "events.error"
	errorEventsCounter, err := meter.Int64Counter(name, api.WithDescription("Number of errors in events processing"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus counter %q: %w", name, err)
	}
	errorEventsCounter.Add(context.Background(), 0)

	name = "nth_tagged_nodes"
	nthTaggedNodesGauge, err := meter.Int64Gauge(name, api.WithDescription("Number of nodes processing"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus gauge %q: %w", name, err)
	}
	nthTaggedNodesGauge.Record(context.Background(), 0)

	name = "nth_tagged_instances"
	nthTaggedInstancesGauge, err := meter.Int64Gauge(name, api.WithDescription("Number of instances processing"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus gauge %q: %w", name, err)
	}
	nthTaggedInstancesGauge.Record(context.Background(), 0)

	return Metrics{
		meter:                   meter,
		errorEventsCounter:      errorEventsCounter,
		actionsCounter:          actionsCounter,
		actionsCounterV2:        actionsCounterV2,
		nthTaggedNodesGauge:     nthTaggedNodesGauge,
		nthTaggedInstancesGauge: nthTaggedInstancesGauge,
	}, nil
}

func serveMetrics(port int) *http.Server {
	http.Handle(metricsEndpoint, promhttp.Handler())

	server := &http.Server{
		Addr: net.JoinHostPort("", strconv.Itoa(port)),
	}

	go func() {
		log.Info().Msgf("Starting to serve handler %s, port %d", metricsEndpoint, port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Err(err).Msg("Failed to listen and serve http server")
		}
	}()

	return server
}
