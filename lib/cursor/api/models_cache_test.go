package cursor_api_sdk

import (
	"testing"
	"time"
)

func TestCachedModelsTTL(t *testing.T) {
	c := &Client{}
	if got := c.CachedModels(); got != nil {
		t.Fatalf("empty cache = %#v", got)
	}

	want := []Model{{ID: "composer-2.5", Name: "Composer 2.5"}}
	c.storeModelCache(want)
	got := c.CachedModels()
	if len(got) != 1 || got[0].ID != "composer-2.5" {
		t.Fatalf("cached = %#v", got)
	}
	got[0].ID = "mutated"
	if c.CachedModels()[0].ID != "composer-2.5" {
		t.Fatal("CachedModels must copy")
	}

	c.mu.Lock()
	c.modelsAt = time.Now().Add(-modelCacheTTL - time.Second)
	c.mu.Unlock()
	if got := c.CachedModels(); got != nil {
		t.Fatalf("expired cache = %#v", got)
	}
}
