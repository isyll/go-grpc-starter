package httpsvc

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/isyll/go-grpc-starter/pkg/config"
)

func withCORS(next http.Handler, cfg config.CORSConfig) http.Handler {
	origins := splitList(cfg.AllowedOrigins)
	methods := strings.Join(splitList(cfg.AllowedMethods), ", ")
	headers := strings.Join(splitList(cfg.AllowedHeaders), ", ")
	wildcard := len(origins) == 1 && origins[0] == "*"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (wildcard || slices.Contains(origins, origin)) {
			if wildcard && !cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			if cfg.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(cfg.MaxAge.Seconds())))
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
