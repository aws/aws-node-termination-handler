package observability

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/api/kv"
	"go.opentelemetry.io/otel/api/metric"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.opentelemetry.io/otel/sdk/metric/controller/pull"
)

var (
	// events labels
	labelEventStatusKey = kv.Key("event/status")
	labelEventKindKey   = kv.Key("event/kind")

	labelEventErrorWhereKey = kv.Key("error/event/where")

	// node labels
	labelNodeActionKey = kv.Key("node/action")
	labelNodeStatusKey = kv.Key("node/status")
	labelNodeNameKey   = kv.Key("node/name")
)

// Metrics holds all the stats
type Metrics struct {
	enabled                 bool
	meter                   metric.Meter
	eventsProcessingCounter metric.Int64UpDownCounter
	actionsCounter          metric.Int64Counter
	errorEventsCounter      metric.Int64Counter
}

// InitMetrics creates/starts the prometheus exporter server and registers the metrics
func InitMetrics(enabled bool, port string) (Metrics, error) {
	if !enabled {
		return Metrics{}, nil
	}

	exporter, err := prometheus.InstallNewPipeline(prometheus.Config{}, pull.WithStateful(false))
	if err != nil {
		return Metrics{}, err
	}

	metrics, err := registerMetricsWith(exporter.Provider())
	if err != nil {
		return Metrics{}, err
	}

	// Starts an async process to collect golang runtime stats
	// go.opentelemetry.io/contrib/instrumentation/runtime
	if err := runtime.Start(metrics.meter, 5*time.Second); err != nil {
		return Metrics{}, err
	}

	// Starts HTTP server exposing the prometheus `/metrics` path
	go func() {
		http.HandleFunc("/metrics", exporter.ServeHTTP)
		err := http.ListenAndServe(":"+port, nil)
		if err != nil {
			log.Err(err).Msg("failed to serve prometheus http server")
		}
	}()

	return metrics, nil
}

// AddEvent only if its enabled and partitioned by status and kind
func (m Metrics) AddEvent(value int64, status, kind string) {
	if !m.enabled {
		return
	}
	m.eventsProcessingCounter.Add(context.Background(), value, labelEventStatusKey.String(status), labelEventKindKey.String(kind))
}

// ErrorEventsInc only if its enabled and partitioned by action
func (m Metrics) ErrorEventsInc(where string) {
	if !m.enabled {
		return
	}
	m.errorEventsCounter.Add(context.Background(), 1, labelEventErrorWhereKey.String(where))
}

// NodeActionsInc only if its enabled and partitioned by action, nodeName and status
func (m Metrics) NodeActionsInc(action, nodeName string, err error) {
	if !m.enabled {
		return
	}

	labels := []kv.KeyValue{labelNodeActionKey.String(action), labelNodeNameKey.String(nodeName)}
	if err != nil {
		labels = append(labels, labelNodeStatusKey.String("error"))
	} else {
		labels = append(labels, labelNodeStatusKey.String("success"))
	}

	m.actionsCounter.Add(context.Background(), 1, labels...)
}

func registerMetricsWith(provider metric.Provider) (Metrics, error) {
	meter := provider.Meter("aws.node.termination.handler")

	actionsCounter, err := meter.NewInt64Counter("actions.node", metric.WithDescription("Number of actions per node"))
	if err != nil {
		return Metrics{}, err
	}

	errorEventsCounter, err := meter.NewInt64Counter("events.error", metric.WithDescription("Number of errors in events processing"))
	if err != nil {
		return Metrics{}, err
	}

	eventsProcessingCounter, err := meter.NewInt64UpDownCounter("events.processing", metric.WithDescription("Actual events processing"))
	if err != nil {
		return Metrics{}, err
	}

	return Metrics{
		enabled:                 true,
		meter:                   meter,
		eventsProcessingCounter: eventsProcessingCounter,
		errorEventsCounter:      errorEventsCounter,
		actionsCounter:          actionsCounter,
	}, nil
}
