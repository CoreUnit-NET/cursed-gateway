package cmdHandler

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

func TestDispatchVersionAndStubs(t *testing.T) {
	var out bytes.Buffer
	rt := &Runtime{Out: &out, Err: &out}

	err := Dispatch(context.Background(), &settings.Settings{
		ShowVersion: true,
		Host:        "127.0.0.1",
		Port:        8080,
		AuthPath:    filepath.Join(t.TempDir(), "data.json"),
		LogLevel:    "info",
		LogFormat:   "text",
	}, "Demo", "1.2.3", "abc", rt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Demo 1.2.3 (abc)") {
		t.Fatalf("version output: %q", out.String())
	}

	err = Dispatch(context.Background(), &settings.Settings{
		Command:    config.CommandModels,
		Host:       "127.0.0.1",
		Port:       8080,
		AuthPath:   filepath.Join(t.TempDir(), "data.json"),
		MaxRetries: 1,
		LogLevel:   "info",
		LogFormat:  "text",
	}, "Demo", "1.2.3", "abc", rt)
	if err == nil {
		t.Fatal("expected models error for empty auth store")
	}
	if !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("models err = %v", err)
	}
}
