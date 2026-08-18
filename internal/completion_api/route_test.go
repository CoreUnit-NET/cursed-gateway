package completion_api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

func TestMountAIPrefixes(t *testing.T) {
	api := &recAPI{cache: []cursor_api_sdk.Model{{ID: "composer-2.5", Name: "Composer 2.5"}}}
	h := &Handler{Server: &Server{Pool: &recPool{}, API: api}}
	mux := http.NewServeMux()
	h.Mount(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/ai/v1/models", "/v1/models", "/models"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, res.StatusCode, body)
		}
		var out modelListResponse
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("GET %s json: %v", path, err)
		}
		if out.Object != "list" || len(out.Data) != 1 || out.Data[0].ID != "composer-2.5" {
			t.Fatalf("GET %s body=%s", path, body)
		}
	}

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status=%d, want 200", res.StatusCode)
	}

	for _, path := range []string{
		"/ai/v1/chat/completions", "/v1/chat/completions", "/chat/completions",
		"/ai/v1/completions", "/v1/completions", "/completions",
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status=%d, want 405 (POST mounted)", path, res.StatusCode)
		}
	}

	for _, path := range []string{"/ai/v1/chat/completions", "/v1/chat/completions", "/chat/completions"} {
		res, body := postJSON(t, srv.URL+path, map[string]string{"model": "x"})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s status=%d body=%s, want 400", path, res.StatusCode, body)
		}
		if !strings.Contains(string(body), "messages is required") {
			t.Fatalf("POST %s body=%s, want messages is required", path, body)
		}
	}

	for _, path := range []string{"/ai/v1/completions", "/v1/completions", "/completions"} {
		res, body := postJSON(t, srv.URL+path, map[string]string{"model": "x"})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s status=%d body=%s, want 400", path, res.StatusCode, body)
		}
		if !strings.Contains(string(body), "No user message found") {
			t.Fatalf("POST %s body=%s, want No user message found", path, body)
		}
	}
}

func postJSON(t *testing.T, url string, payload any) (*http.Response, []byte) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}
