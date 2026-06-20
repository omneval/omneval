package server

import (
	"net/http"

	"github.com/omneval/omneval/internal/probe"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// newHTTPMux builds the Quack Server's health/readiness/metrics mux, served
// on cfg.Metrics.Addr (quack.client/server share the "metrics" port name,
// :9090 by default) alongside the quack_serve() protocol listener.
func newHTTPMux(p *probe.Prober) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/healthz", p.HealthHandler())
	mux.Handle("/readyz", p.ReadyHandler())
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
