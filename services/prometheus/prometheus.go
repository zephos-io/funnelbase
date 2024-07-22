package prometheus

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"zephos/funnelbase/util"
)

var (
	logger = util.NewLogger().With().Str("component", "prometheus").Logger()
)

func ListenAndServe() {
	logger.Info().Msg("starting prometheus server")
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe("0.0.0.0:2112", nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("error starting http server")
	}
}
