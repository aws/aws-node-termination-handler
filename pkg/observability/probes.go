package observability

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// InitProbes will initialize, register and expose, via http server, the probes.
func InitProbes(enabled bool, port int, endpoint string) error {
	if !enabled {
		return nil
	}

	http.HandleFunc(endpoint, livenessHandler)

	probes := &http.Server{
		Addr:         net.JoinHostPort("", strconv.Itoa(port)),
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}

	// Starts HTTP server exposing the probes path
	go func() {
		log.Info().Msgf("Starting to serve handler %s, port %d", endpoint, port)
		if err := probes.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Err(err).Msg("Failed to listen and serve http server")
		}
	}()

	return nil
}

func livenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"health":"OK"}`))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Warn().Err(err).Msg("Unable to write health response")
	}
}
