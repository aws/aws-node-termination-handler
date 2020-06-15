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
	labelEventActionKey = kv.Key("event/action")
	labelEventKindKey   = kv.Key("event/kind")

	// node labels
	labelNodeActionKey = kv.Key("node/action")
	labelNodeStatusKey = kv.Key("node/status")
	labelNodeValueKey  = kv.Key("node/value")
)

// Metrics holds all the stats
type Metrics struct {
	enabled     bool
	meter       metric.Meter
	watchEvents metric.Int64Counter
	nodeActions metric.Int64Counter
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

	// Starts HTTP server exposing the `/metrics` path for prometheus scrapper
	go func() {
		http.HandleFunc("/metrics", exporter.ServeHTTP)
		err := http.ListenAndServe(":"+port, nil)
		if err != nil {
			log.Err(err).Msg("failed to serve prometheus http server")
		}
	}()

	return metrics, nil
}

// WatchEventsInc adds one only if its enabled and partitioned by action and kind
func (m Metrics) WatchEventsInc(action, kind string) {
	if !m.enabled {
		return
	}
	m.watchEvents.Add(context.Background(), 1, labelEventActionKey.String(action), labelEventKindKey.String(kind))
}

// NodeActionsInc adds one only if its enabled and partitioned by action, node and status
func (m Metrics) NodeActionsInc(action, node string, err error) {
	if !m.enabled {
		return
	}

	labels := []kv.KeyValue{labelNodeActionKey.String(action), labelNodeValueKey.String(node)}
	if err != nil {
		labels = append(labels, labelNodeStatusKey.String("error"))
	} else {
		labels = append(labels, labelNodeStatusKey.String("success"))
	}

	m.nodeActions.Add(context.Background(), 1, labels...)
}

func registerMetricsWith(provider metric.Provider) (Metrics, error) {
	meter := provider.Meter("aws.node.termination.handler")

	watch, err := meter.NewInt64Counter(
		"watch.events",
		metric.WithDescription("Number of events watched"),
	)
	if err != nil {
		return Metrics{}, err
	}

	action, err := meter.NewInt64Counter(
		"actions.node",
		metric.WithDescription("Number of actions per node"),
	)
	if err != nil {
		return Metrics{}, err
	}

	return Metrics{
		enabled:     true,
		meter:       meter,
		watchEvents: watch,
		nodeActions: action,
	}, nil
}
