package cursor_api_sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ConversationIdentity carries OpenAI-like sticky ids (otto conversation/identity.ts).
// First non-empty wins: conversation_id → thread_id → session_id → user → metadata.*.
type ConversationIdentity struct {
	ConversationID string
	ThreadID       string
	SessionID      string
	User           string
	Metadata       map[string]any
}

// BuildConversationIdentity returns otto-style "id:…" / "meta:…" or "" if none.
func BuildConversationIdentity(id ConversationIdentity) string {
	for _, v := range []string{id.ConversationID, id.ThreadID, id.SessionID, id.User} {
		if s := strings.TrimSpace(v); s != "" {
			return "id:" + s
		}
	}
	if id.Metadata == nil {
		return ""
	}
	for _, key := range []string{"conversation_id", "thread_id", "session_id", "chat_id", "id"} {
		v, ok := id.Metadata[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			return "meta:" + key + ":" + s
		}
	}
	return ""
}

// DeriveConversationKey hashes a non-empty identity seed (model-independent).
// Empty identity returns "".
func DeriveConversationKey(identity string) string {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("conv:" + identity))
	return hex.EncodeToString(sum[:])[:24]
}

// DeterministicConversationID builds a UUID-shaped id from a conversation key
// (otto deterministicConversationId) so Cursor can stick the conversation.
func DeterministicConversationID(convKey string) string {
	convKey = strings.TrimSpace(convKey)
	if convKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("cursor-conv-id:" + convKey))
	h := hex.EncodeToString(sum[:])
	if len(h) < 32 {
		return ""
	}
	h = h[:32]
	nibble, err := strconv.ParseInt(string(h[16]), 16, 0)
	if err != nil {
		nibble = 0
	}
	variant := fmt.Sprintf("%x", 0x8|int(nibble&0x3))
	return fmt.Sprintf("%s-%s-4%s-%s%s-%s", h[0:8], h[8:12], h[13:16], variant, h[17:20], h[20:32])
}

// StickyConversationID returns a deterministic Cursor conversation_id when the
// client sent an OpenAI-like sticky id; otherwise "" (caller should use random).
// Deliberately skips weak first-user-hash fallback (support.md §4.9).
func StickyConversationID(id ConversationIdentity) string {
	ident := BuildConversationIdentity(id)
	if ident == "" {
		return ""
	}
	return DeterministicConversationID(DeriveConversationKey(ident))
}

// DeriveBridgeKeyWithIdentity prefers sticky identity for tool-park keys;
// falls back to DeriveBridgeKey (first-user hash) when identity is empty.
func DeriveBridgeKeyWithIdentity(modelID string, messages []ChatMessage, id ConversationIdentity) string {
	if ident := BuildConversationIdentity(id); ident != "" {
		sum := sha256.Sum256([]byte("bridge:" + modelID + ":" + ident))
		return hex.EncodeToString(sum[:])[:16]
	}
	return DeriveBridgeKey(modelID, messages)
}
