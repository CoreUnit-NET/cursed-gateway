package service

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWrapMuxJSONNotFoundAndMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
	})
	mux.HandleFunc("POST /ai/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	var logBuf bytes.Buffer
	srv := httptest.NewServer(wrapMux(mux, slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))))
	t.Cleanup(srv.Close)
	client := srv.Client()

	res, body := doHTTP(t, client, http.MethodGet, srv.URL+"/login", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /login status=%d body=%s, want 404", res.StatusCode, body)
	}
	assertControlError(t, res, body, "not found")

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/ai/v1/nope", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /ai/v1/nope status=%d body=%s, want 404", res.StatusCode, body)
	}
	assertOpenAIError(t, res, body, "not found", "not_found")

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/v1/unknown", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /v1/unknown status=%d body=%s, want 404", res.StatusCode, body)
	}
	assertOpenAIError(t, res, body, "not found", "not_found")

	res, body = doHTTP(t, client, http.MethodPost, srv.URL+"/api", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api status=%d body=%s, want 405", res.StatusCode, body)
	}
	assertControlError(t, res, body, "method not allowed")
	if got := res.Header.Get("Allow"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("POST /api Allow=%q, want GET", got)
	}

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/ai/v1/chat/completions", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET chat status=%d body=%s, want 405", res.StatusCode, body)
	}
	assertOpenAIError(t, res, body, "method not allowed", "method_not_allowed")
	if got := res.Header.Get("Allow"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("GET chat Allow=%q, want POST", got)
	}

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/healthz", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status=%d body=%s, want 200", res.StatusCode, body)
	}
	if string(body) != "ok\n" {
		t.Fatalf("GET /healthz body=%q", body)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "msg=request") {
		t.Fatalf("expected access log, got %q", logged)
	}
	if !strings.Contains(logged, "path=/login") || !strings.Contains(logged, "status=404") {
		t.Fatalf("expected /login 404 access log, got %q", logged)
	}
	if strings.Contains(logged, "path=/healthz") {
		t.Fatalf("healthz must not log at info, got %q", logged)
	}
}

func TestWrapMuxHealthzAccessLogIsDebug(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	var logBuf bytes.Buffer
	h := wrapMux(mux, slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "level=DEBUG") || !strings.Contains(logged, "path=/healthz") {
		t.Fatalf("expected debug healthz access log, got %q", logged)
	}
}

func doHTTP(t *testing.T, client *http.Client, method, url string, body []byte) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, got
}

func assertControlError(t *testing.T, res *http.Response, body []byte, want string) {
	t.Helper()
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type=%q, want json; body=%s", ct, body)
	}
	var got controlMuxError
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("control error json: %v body=%s", err, body)
	}
	if got.Error != want {
		t.Fatalf("error=%q, want %q body=%s", got.Error, want, body)
	}
}

func assertOpenAIError(t *testing.T, res *http.Response, body []byte, wantMsg, wantCode string) {
	t.Helper()
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type=%q, want json; body=%s", ct, body)
	}
	var got openaiMuxError
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("openai error json: %v body=%s", err, body)
	}
	if got.Error.Message != wantMsg || got.Error.Type != "invalid_request_error" || got.Error.Code != wantCode {
		t.Fatalf("error=%+v, want message=%q code=%q body=%s", got.Error, wantMsg, wantCode, body)
	}
}
