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
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	api "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/sdk/metric"
)

var (
	labelEventErrorWhereKey = attribute.Key("event/error/where")

	labelNodeActionKey = attribute.Key("node/action")
	labelNodeStatusKey = attribute.Key("node/status")
	labelNodeNameKey   = attribute.Key("node/name")
	labelEventIDKey    = attribute.Key("node/event-id")
)

// Metrics represents the stats for observability
type Metrics struct {
	enabled            bool
	meter              api.Meter
	actionsCounter     instrument.Int64Counter
	errorEventsCounter instrument.Int64Counter
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
	if err = runtime.Start(
		runtime.WithMeterProvider(provider),
		runtime.WithMinimumReadMemStatsInterval(1*time.Second)); err != nil {
		return Metrics{}, fmt.Errorf("failed to start Go runtime metrics collection: %w", err)
	}

	go serveMetrics(port)

	return metrics, nil
}

// ErrorEventsInc will increment one for the event errors counter, partitioned by action, and only if metrics are enabled.
func (m Metrics) ErrorEventsInc(where string) {
	if !m.enabled {
		return
	}
	m.errorEventsCounter.Add(context.Background(), 1, labelEventErrorWhereKey.String(where))
}

// NodeActionsInc will increment one for the node stats counter, partitioned by action, nodeName and status, and only if metrics are enabled.
func (m Metrics) NodeActionsInc(action, nodeName string, eventID string, err error) {
	if !m.enabled {
		return
	}

	labels := []attribute.KeyValue{labelNodeActionKey.String(action), labelNodeNameKey.String(nodeName), labelEventIDKey.String(eventID)}
	if err != nil {
		labels = append(labels, labelNodeStatusKey.String("error"))
	} else {
		labels = append(labels, labelNodeStatusKey.String("success"))
	}

	m.actionsCounter.Add(context.Background(), 1, labels...)
}

func registerMetricsWith(provider *metric.MeterProvider) (Metrics, error) {
	meter := provider.Meter("aws.node.termination.handler")

	name := "actions.node"
	actionsCounter, err := meter.Int64Counter(name, instrument.WithDescription("Number of actions per node"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus counter %q: %w", name, err)
	}
	actionsCounter.Add(context.Background(), 0)

	name = "events.error"
	errorEventsCounter, err := meter.Int64Counter(name, instrument.WithDescription("Number of errors in events processing"))
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create Prometheus counter %q: %w", name, err)
	}
	errorEventsCounter.Add(context.Background(), 0)
	return Metrics{
		meter:              meter,
		errorEventsCounter: errorEventsCounter,
		actionsCounter:     actionsCounter,
	}, nil
}

func serveMetrics(port int) {
	log.Info().Msgf("Starting to serve handler /metrics, port %d", port)
	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Err(err).Msg("Failed to listen and serve http server")
	}
}
