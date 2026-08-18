package cursor_api_sdk

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

const defaultCheckpointTTL = 10 * time.Minute

// CheckpointMode describes how a prior sticky checkpoint was applied.
const (
	CheckpointMiss    = "miss"    // no stored checkpoint for identity
	CheckpointHit     = "hit"     // system blob head matched; non-history merged
	CheckpointRebuild = "rebuild" // stored but system prompt changed
)

// StoredCheckpoint is an identity-keyed capture of ConversationStateStructure
// plus companion blobs referenced by that state (oauth proxy.ts 1477–1486).
type StoredCheckpoint struct {
	State *cursorProto.ConversationStateStructure
	Blobs map[string][]byte
}

// CheckpointStore is an in-memory sticky checkpoint cache (TTL like tool bridges).
// Keys must come from DeriveConversationKey(identity); empty identity must not store.
type CheckpointStore struct {
	mu   sync.Mutex
	byID map[string]*storedCheckpoint
	ttl  time.Duration
}

type storedCheckpoint struct {
	state     *cursorProto.ConversationStateStructure
	blobs     map[string][]byte
	expiresAt time.Time
}

// NewCheckpointStore builds a TTL'd in-memory store.
func NewCheckpointStore(ttl time.Duration) *CheckpointStore {
	if ttl <= 0 {
		ttl = defaultCheckpointTTL
	}
	return &CheckpointStore{byID: map[string]*storedCheckpoint{}, ttl: ttl}
}

// Put captures/replaces checkpoint state for key, merging live blobs into the entry.
func (s *CheckpointStore) Put(key string, state *cursorProto.ConversationStateStructure, liveBlobs map[string][]byte) {
	if s == nil || key == "" || state == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())

	entry := s.byID[key]
	if entry == nil {
		entry = &storedCheckpoint{blobs: map[string][]byte{}}
		s.byID[key] = entry
	}
	cloned, ok := proto.Clone(state).(*cursorProto.ConversationStateStructure)
	if !ok || cloned == nil {
		return
	}
	entry.state = cloned
	if entry.blobs == nil {
		entry.blobs = map[string][]byte{}
	}
	for k, v := range liveBlobs {
		entry.blobs[k] = append([]byte(nil), v...)
	}
	entry.expiresAt = time.Now().Add(s.ttl)
}

// Get returns a deep copy of the stored checkpoint, or nil on miss/expiry.
func (s *CheckpointStore) Get(key string) *StoredCheckpoint {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	entry := s.byID[key]
	if entry == nil || entry.state == nil {
		return nil
	}
	cloned, ok := proto.Clone(entry.state).(*cursorProto.ConversationStateStructure)
	if !ok || cloned == nil {
		return nil
	}
	return &StoredCheckpoint{
		State: cloned,
		Blobs: cloneBlobStore(entry.blobs),
	}
}

func (s *CheckpointStore) expireLocked(now time.Time) {
	for k, e := range s.byID {
		if e == nil || now.After(e.expiresAt) {
			delete(s.byID, k)
		}
	}
}

func cloneBlobStore(in map[string][]byte) map[string][]byte {
	if len(in) == 0 {
		return map[string][]byte{}
	}
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// SeedBlobStore copies src blobs into dst without removing existing keys.
func SeedBlobStore(dst map[string][]byte, src map[string][]byte) {
	if dst == nil {
		return
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = append([]byte(nil), v...)
	}
}

// SystemPromptMatches reports whether checkpoint root head equals systemBlobIDs
// (oauth proxy.ts 808–815).
func SystemPromptMatches(checkpoint *cursorProto.ConversationStateStructure, systemBlobIDs [][]byte) bool {
	if checkpoint == nil {
		return false
	}
	root := checkpoint.GetRootPromptMessagesJson()
	if len(root) < len(systemBlobIDs) || len(systemBlobIDs) == 0 {
		return false
	}
	for i, id := range systemBlobIDs {
		if !bytes.Equal(root[i], id) {
			return false
		}
	}
	return true
}

// SystemPromptCompatible is true when blob IDs match (oauth) OR when the
// checkpoint's first root blob decodes to the same system prompt text.
// The text fallback covers server-echoed checkpoints that re-blobify root.
func SystemPromptCompatible(
	checkpoint *cursorProto.ConversationStateStructure,
	systemBlobIDs [][]byte,
	blobs map[string][]byte,
	systemPrompt string,
) bool {
	if SystemPromptMatches(checkpoint, systemBlobIDs) {
		return true
	}
	if checkpoint == nil || len(checkpoint.GetRootPromptMessagesJson()) == 0 {
		return false
	}
	raw := blobs[hex.EncodeToString(checkpoint.RootPromptMessagesJson[0])]
	if len(raw) == 0 {
		return false
	}
	var msg struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(msg.Role)) != "system" {
		return false
	}
	return rootContentText(msg.Content) == systemPrompt
}

func rootContentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, part := range c {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t != "" && t != "text" && t != "input_text" {
				continue
			}
			if text, ok := m["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// MergeCheckpointState keeps non-history fields from checkpoint and always
// replaces rootPromptMessagesJson + turns with freshly blobified history
// (oauth proxy.ts 819–824).
func MergeCheckpointState(checkpoint *cursorProto.ConversationStateStructure, root, turns [][]byte) *cursorProto.ConversationStateStructure {
	if checkpoint == nil {
		return &cursorProto.ConversationStateStructure{
			RootPromptMessagesJson: root,
			Turns:                  turns,
			FileStates:             map[string][]byte{},
			FileStatesV2:           map[string]*cursorProto.FileStateStructure{},
			SubagentStates:         map[string]*cursorProto.SubagentPersistedState{},
		}
	}
	cloned, ok := proto.Clone(checkpoint).(*cursorProto.ConversationStateStructure)
	if !ok || cloned == nil {
		return &cursorProto.ConversationStateStructure{
			RootPromptMessagesJson: root,
			Turns:                  turns,
			FileStates:             map[string][]byte{},
			FileStatesV2:           map[string]*cursorProto.FileStateStructure{},
			SubagentStates:         map[string]*cursorProto.SubagentPersistedState{},
		}
	}
	cloned.RootPromptMessagesJson = root
	cloned.Turns = turns
	return cloned
}
