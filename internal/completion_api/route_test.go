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

	for _, path := range []string{"/healthz", "/health"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d, want 200", path, res.StatusCode)
		}
		if string(body) != "ok\n" {
			t.Fatalf("GET %s body=%q", path, body)
		}
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
		assertOpenAIErrorParamNull(t, body)
	}

	for _, path := range []string{"/ai/v1/completions", "/v1/completions", "/completions"} {
		res, body := postJSON(t, srv.URL+path, map[string]string{"model": "x"})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s status=%d body=%s, want 400", path, res.StatusCode, body)
		}
		if !strings.Contains(string(body), "prompt is required") {
			t.Fatalf("POST %s body=%s, want prompt is required", path, body)
		}
		assertOpenAIErrorParamNull(t, body)
	}
}

func assertOpenAIErrorParamNull(t *testing.T, body []byte) {
	t.Helper()
	var wrap struct {
		Error struct {
			Param any `json:"param"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatalf("error json: %v body=%s", err, body)
	}
	if wrap.Error.Param != nil {
		t.Fatalf("error.param=%#v, want null body=%s", wrap.Error.Param, body)
	}
	if !bytes.Contains(body, []byte(`"param":null`)) {
		t.Fatalf("error body missing \"param\":null: %s", body)
	}
}

func TestMountAIValidationAndUnknown(t *testing.T) {
	api := &recAPI{cache: []cursor_api_sdk.Model{{ID: "composer-2.5", Name: "Composer 2.5"}}}
	h := &Handler{Server: &Server{Pool: &recPool{}, API: api}}
	mux := http.NewServeMux()
	h.Mount(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, body := postJSON(t, srv.URL+"/ai/v1/chat/completions", map[string]any{})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty json status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "messages is required") {
		t.Fatalf("empty json body=%s, want messages is required", body)
	}

	res, body = postJSON(t, srv.URL+"/v1/chat/completions", map[string]any{
		"model": "x",
		"messages": []map[string]string{
			{"role": "system", "content": "only system"},
		},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("system-only status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "No user message found") {
		t.Fatalf("system-only body=%s, want No user message found", body)
	}

	res, body = postRaw(t, srv.URL+"/chat/completions", "application/json", []byte("{"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("bad json body=%s, want invalid JSON", body)
	}

	res, body = postRaw(t, srv.URL+"/ai/v1/chat/completions", "application/json", []byte(""))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty chat body status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("empty chat body=%s, want invalid JSON", body)
	}

	res, body = postJSON(t, srv.URL+"/ai/v1/completions", map[string]any{})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty completions json status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "prompt is required") {
		t.Fatalf("empty completions json body=%s, want prompt is required", body)
	}

	res, body = postRaw(t, srv.URL+"/v1/completions", "application/json", []byte("{"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad completions json status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("bad completions json body=%s, want invalid JSON", body)
	}

	res, body = postRaw(t, srv.URL+"/completions", "application/json", []byte(""))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty completions body status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("empty completions body=%s, want invalid JSON", body)
	}

	for _, path := range []string{"/ai/v1/models", "/v1/models", "/models"} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status=%d, want 405", path, res.StatusCode)
		}
	}

	for _, path := range []string{"/ai/v1/nope", "/v1/unknown", "/not-a-route"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status=%d, want 404", path, res.StatusCode)
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

func postRaw(t *testing.T, url, contentType string, data []byte) (*http.Response, []byte) {
	t.Helper()
	res, err := http.Post(url, contentType, bytes.NewReader(data))
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
