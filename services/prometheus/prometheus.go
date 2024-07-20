package prometheus

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

func ListenAndServe() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe("0.0.0.0:2112", nil)
}
