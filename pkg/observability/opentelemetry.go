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
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.opentelemetry.io/otel/metric"
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
	meter              metric.Meter
	actionsCounter     metric.Int64Counter
	errorEventsCounter metric.Int64Counter
}

// InitMetrics will initialize, register and expose, via http server, the metrics with Opentelemetry.
func InitMetrics(enabled bool, port int) (Metrics, error) {
	if !enabled {
		return Metrics{}, nil
	}

	exporter, err := prometheus.InstallNewPipeline(prometheus.Config{})
	if err != nil {
		return Metrics{}, err
	}

	metrics, err := registerMetricsWith(exporter.MeterProvider())
	if err != nil {
		return Metrics{}, err
	}

	// Starts an async process to collect golang runtime stats
	// go.opentelemetry.io/contrib/instrumentation/runtime
	if err := runtime.Start(
		runtime.WithMeterProvider(exporter.MeterProvider()),
		runtime.WithMinimumReadMemStatsInterval(1*time.Second)); err != nil {
		return Metrics{}, err
	}

	// Starts HTTP server exposing the prometheus `/metrics` path
	go func() {
		log.Info().Msgf("Starting to serve handler /metrics, port %d", port)
		http.HandleFunc("/metrics", exporter.ServeHTTP)
		err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
		if err != nil {
			log.Err(err).Msg("Failed to listen and serve http server")
		}
	}()

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

func registerMetricsWith(provider metric.MeterProvider) (Metrics, error) {
	meter := provider.Meter("aws.node.termination.handler")

	actionsCounter, err := meter.NewInt64Counter("actions.node", metric.WithDescription("Number of actions per node"))
	if err != nil {
		return Metrics{}, err
	}

	errorEventsCounter, err := meter.NewInt64Counter("events.error", metric.WithDescription("Number of errors in events processing"))
	if err != nil {
		return Metrics{}, err
	}

	return Metrics{
		enabled:            true,
		meter:              meter,
		errorEventsCounter: errorEventsCounter,
		actionsCounter:     actionsCounter,
	}, nil
}
