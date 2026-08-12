package cursor_api_sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteCatalogFixtures(t *testing.T) {
	if os.Getenv("WRITE_CATALOG_FIXTURES") == "" {
		t.Skip("set WRITE_CATALOG_FIXTURES=1 to refresh testdata fixtures")
	}
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "data", "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var store struct {
		Sessions []struct {
			Access string `json:"access"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &store); err != nil || len(store.Sessions) == 0 {
		t.Fatal("no session")
	}
	c := &Client{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := []byte(`{"includeLongContextModels":true,"useModelParameters":true,"useCloudAgentEffortModes":true}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+pathAvailableModels, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	c.setCommonHeaders(req.Header, store.Sessions[0].Access)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	res, err := c.httpClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatalf("status %d: %s", res.StatusCode, rawBody)
	}
	var parsed struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"claude-haiku-4-5": "claude-haiku-4-5.json",
		"gemini-3.6-flash": "gemini-3.6-flash.json",
		"gpt-5.4":          "gpt-5.4.json",
		"composer-2.5":     "composer-2.5.json",
	}
	dir := filepath.Join("testdata", "catalog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rm := range parsed.Models {
		var meta struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(rm, &meta)
		name, ok := want[meta.Name]
		if !ok {
			continue
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, rm, "", "  "); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
	}
}
