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

	"github.com/CoreUnit-NET/cursed-gateway/ui"
)

func TestWrapMuxJSONNotFoundAndMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
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

	res, body = doHTTP(t, client, http.MethodPost, srv.URL+"/api/status", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/status status=%d body=%s, want 405", res.StatusCode, body)
	}
	assertControlError(t, res, body, "method not allowed")
	if got := res.Header.Get("Allow"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("POST /api/status Allow=%q, want GET", got)
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

func TestWrapMuxTrailingSlashAIAndHealth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ai/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ai/v1/models" {
			t.Fatalf("models path=%q, want trimmed", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("health path=%q, want trimmed", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
	})

	h := wrapMux(mux, slog.New(slog.NewTextHandler(io.Discard, nil)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ai/v1/models/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ai/v1/models/ status=%d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health/ status=%d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz/ status=%d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/status/ status=%d, want 404 (control paths untrimmed)", rec.Code)
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

func TestMountUIServesIndexAndAssets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
	})
	mountUI(mux, ui.FS)

	srv := httptest.NewServer(wrapMux(mux, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	client := srv.Client()

	res, body := doHTTP(t, client, http.MethodGet, srv.URL+"/", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / status=%d body=%s, want 200", res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / content-type=%q, want html", ct)
	}
	if !strings.Contains(string(body), "<!DOCTYPE html>") && !strings.Contains(string(body), "<html") {
		snippet := string(body)
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		t.Fatalf("GET / body missing html, got %q", snippet)
	}

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/css/app.css", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /css/app.css status=%d body=%s, want 200", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "--bg:") {
		snippet := string(body)
		if len(snippet) > 80 {
			snippet = snippet[:80]
		}
		t.Fatalf("GET /css/app.css unexpected body prefix %q", snippet)
	}

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/js/app.js", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /js/app.js status=%d body=%s, want 200", res.StatusCode, body)
	}

	res, body = doHTTP(t, client, http.MethodGet, srv.URL+"/api/status", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status=%d body=%s, want 200", res.StatusCode, body)
	}
	if string(body) != `{"ok":true}`+"\n" {
		t.Fatalf("GET /api/status body=%q", body)
	}

	noFollow := *client
	noFollow.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	res, body = doHTTP(t, &noFollow, http.MethodGet, srv.URL+"/index.html", nil)
	if res.StatusCode != http.StatusFound {
		t.Fatalf("GET /index.html status=%d body=%s, want 302", res.StatusCode, body)
	}
	if loc := res.Header.Get("Location"); loc != "/" {
		t.Fatalf("GET /index.html Location=%q, want /", loc)
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
