package service

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var probeMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

type openaiMuxError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type controlMuxError struct {
	Error string `json:"error"`
}

// wrapMux logs each handled request and returns JSON 404/405 for unmatched mux routes.
func wrapMux(mux *http.ServeMux, log *slog.Logger) http.Handler {
	if mux == nil {
		mux = http.NewServeMux()
	}
	if log == nil {
		log = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		if r != nil {
			if _, pattern := mux.Handler(r); pattern == "" {
				if allow := allowedMethods(mux, r); len(allow) > 0 {
					rec.Header().Set("Allow", strings.Join(allow, ", "))
					writeMuxError(rec, r, http.StatusMethodNotAllowed, "method not allowed")
				} else {
					writeMuxError(rec, r, http.StatusNotFound, "not found")
				}
			} else {
				mux.ServeHTTP(rec, r)
			}
		}
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		logRequest(log, r, status, time.Since(start))
	})
}

func logRequest(log *slog.Logger, r *http.Request, status int, d time.Duration) {
	if log == nil {
		return
	}
	method, path := "", ""
	if r != nil {
		method = r.Method
		if r.URL != nil {
			path = r.URL.Path
		}
	}
	attrs := []any{"method", method, "path", path, "status", status, "ms", d.Milliseconds()}
	if path == "/healthz" {
		log.Debug("request", attrs...)
		return
	}
	log.Info("request", attrs...)
}

func allowedMethods(mux *http.ServeMux, r *http.Request) []string {
	if mux == nil || r == nil {
		return nil
	}
	clone := r.Clone(r.Context())
	var allow []string
	for _, m := range probeMethods {
		clone.Method = m
		if _, pattern := mux.Handler(clone); pattern != "" {
			allow = append(allow, m)
		}
	}
	return allow
}

func writeMuxError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if r != nil && isAIPath(r.URL.Path) {
		var body openaiMuxError
		body.Error.Message = msg
		body.Error.Type = "invalid_request_error"
		switch status {
		case http.StatusNotFound:
			body.Error.Code = "not_found"
		case http.StatusMethodNotAllowed:
			body.Error.Code = "method_not_allowed"
		default:
			body.Error.Code = "bad_request"
		}
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, status, controlMuxError{Error: msg})
}

func isAIPath(path string) bool {
	switch {
	case path == "/ai" || strings.HasPrefix(path, "/ai/"):
		return true
	case path == "/v1" || strings.HasPrefix(path, "/v1/"):
		return true
	case path == "/models" || strings.HasPrefix(path, "/models/"):
		return true
	case path == "/chat/completions" || strings.HasPrefix(path, "/chat/"):
		return true
	case path == "/completions" || strings.HasPrefix(path, "/completions/"):
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
