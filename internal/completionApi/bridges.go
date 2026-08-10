package completion_api

import (
	"sync"
	"time"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

const bridgeTTL = 10 * time.Minute

// liveBridge parks a Cursor AgentService/Run across OpenAI tool turns.
type liveBridge struct {
	RC        *cursor_api_sdk.RunControl
	ModelID   string
	ExpiresAt time.Time
}

type bridgeRegistry struct {
	mu   sync.Mutex
	byID map[string]*liveBridge
}

func newBridgeRegistry() *bridgeRegistry {
	return &bridgeRegistry{byID: map[string]*liveBridge{}}
}

func (s *Server) bridges() *bridgeRegistry {
	if s == nil {
		return newBridgeRegistry()
	}
	s.bridgeOnce.Do(func() {
		s.activeBridges = newBridgeRegistry()
	})
	return s.activeBridges
}

func (r *bridgeRegistry) park(key string, rc *cursor_api_sdk.RunControl, modelID string) {
	if r == nil || key == "" || rc == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.byID[key]; ok && old != nil && old.RC != nil && old.RC != rc {
		old.RC.Close()
	}
	r.byID[key] = &liveBridge{
		RC:        rc,
		ModelID:   modelID,
		ExpiresAt: time.Now().Add(bridgeTTL),
	}
}

func (r *bridgeRegistry) take(key string) *liveBridge {
	if r == nil || key == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())
	b := r.byID[key]
	if b == nil {
		return nil
	}
	delete(r.byID, key)
	return b
}

func (r *bridgeRegistry) drop(key string) {
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.byID[key]; ok && old != nil && old.RC != nil {
		old.RC.Close()
	}
	delete(r.byID, key)
}

func (r *bridgeRegistry) expireLocked(now time.Time) {
	for k, b := range r.byID {
		if b == nil || now.After(b.ExpiresAt) {
			if b != nil && b.RC != nil {
				b.RC.Close()
			}
			delete(r.byID, k)
		}
	}
}
