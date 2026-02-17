package server

import (
	"net/http"
	"time"
)

// MetricsMiddleware records request count and duration metrics.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(recorder, r)

		ObserveHTTPRequest(r.Method, r.URL.Path, recorder.statusCode, time.Since(start))
	})
}
