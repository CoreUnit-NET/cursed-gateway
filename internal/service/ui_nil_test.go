package service

import (
	"context"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	"net/http"
	"path/filepath"
	"testing"
)

// TestRunServeWithoutUI ensures that Serve can start without a UI filesystem
func TestRunServeWithoutUI(t *testing.T) {
	tempDir := t.TempDir()
	s := &settings.Settings{Host: "127.0.0.1", Port: 8080, AuthPath: filepath.Join(tempDir, "store.json")}
	// Use a background context that will cancel after a short timeout
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the server in a goroutine
	go func() {
		if err := RunServe(ctx, s, nil, nil); err != nil {
			t.Fatalf("RunServe returned error: %v", err)
		}
	}()

	// Wait a moment for the server to start
	client := &http.Client{}
	var lastErr error
	for i := 0; i < 10; i++ {
		resp, err := client.Get("http://127.0.0.1:8080/api/status")
		if err == nil {
			resp.Body.Close()
			return
		}
		lastErr = err
	}
	t.Fatalf("server did not start: %v", lastErr)
}
