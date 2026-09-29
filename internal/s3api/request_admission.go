package s3api

import (
	"fmt"
	"net/http"
)

// DefaultMaxConcurrentRequests bounds active handlers before body buffering and authentication.
const DefaultMaxConcurrentRequests = 16

func normalizeRequestLimits(cfg *Config) error {
	if cfg.MaxRequestBytes < 0 || cfg.MaxConcurrentRequests < 0 {
		return fmt.Errorf("s3api: request limits must not be negative")
	}
	if cfg.MaxRequestBytes == 0 {
		cfg.MaxRequestBytes = DefaultMaxRequestBytes
	}
	if cfg.MaxConcurrentRequests == 0 {
		cfg.MaxConcurrentRequests = DefaultMaxConcurrentRequests
	}
	return nil
}

func (s *Server) admitRequest(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.requestSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, r, s3Error{Code: "SlowDown", Message: "maximum concurrent requests reached", StatusCode: http.StatusServiceUnavailable})
		return false
	}
}
