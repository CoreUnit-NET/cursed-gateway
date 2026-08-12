package completion_api

import (
	"testing"
	"time"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

func TestBridgeRegistryParkTakeDrop(t *testing.T) {
	s := &Server{}
	reg := s.bridges()
	rc := &cursor_api_sdk.RunControl{}

	reg.park("k1", rc, "model-a")
	got := reg.take("k1")
	if got == nil || got.RC != rc || got.ModelID != "model-a" {
		t.Fatalf("take = %#v", got)
	}
	if reg.take("k1") != nil {
		t.Fatal("expected take to remove bridge")
	}

	reg.park("k2", rc, "model-b")
	reg.drop("k2")
	if reg.take("k2") != nil {
		t.Fatal("expected drop to remove bridge")
	}
}

func TestBridgeRegistryExpire(t *testing.T) {
	reg := newBridgeRegistry(nil)
	rc := &cursor_api_sdk.RunControl{}
	reg.park("old", rc, "m")
	reg.mu.Lock()
	reg.byID["old"].ExpiresAt = time.Now().Add(-time.Second)
	reg.mu.Unlock()
	if reg.take("old") != nil {
		t.Fatal("expected expired bridge to be removed")
	}
}
