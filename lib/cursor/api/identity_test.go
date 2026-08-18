package cursor_api_sdk

import (
	"strings"
	"testing"
)

func TestBuildConversationIdentityPriority(t *testing.T) {
	got := BuildConversationIdentity(ConversationIdentity{
		ConversationID: "c1",
		ThreadID:       "t1",
		SessionID:      "s1",
		User:           "u1",
	})
	if got != "id:c1" {
		t.Fatalf("got %q, want id:c1", got)
	}
	got = BuildConversationIdentity(ConversationIdentity{ThreadID: " t1 "})
	if got != "id:t1" {
		t.Fatalf("got %q", got)
	}
	got = BuildConversationIdentity(ConversationIdentity{
		Metadata: map[string]any{"session_id": "meta-s", "chat_id": "chat"},
	})
	if got != "meta:session_id:meta-s" {
		t.Fatalf("got %q", got)
	}
	got = BuildConversationIdentity(ConversationIdentity{
		Metadata: map[string]any{"id": 123},
	})
	if got != "" {
		t.Fatalf("non-string metadata should be ignored, got %q", got)
	}
	if BuildConversationIdentity(ConversationIdentity{}) != "" {
		t.Fatal("empty identity should be empty")
	}
}

func TestStickyConversationIDStable(t *testing.T) {
	id := ConversationIdentity{ConversationID: "sticky-abc"}
	a := StickyConversationID(id)
	b := StickyConversationID(id)
	if a == "" || a != b {
		t.Fatalf("sticky ids differ or empty: %q vs %q", a, b)
	}
	parts := strings.Split(a, "-")
	if len(parts) != 5 {
		t.Fatalf("uuid shape = %q", a)
	}
	if len(parts[2]) != 4 || parts[2][0] != '4' {
		t.Fatalf("version nibble = %q", parts[2])
	}
	other := StickyConversationID(ConversationIdentity{ConversationID: "sticky-xyz"})
	if other == a {
		t.Fatal("different identities must yield different conversation ids")
	}
	if StickyConversationID(ConversationIdentity{}) != "" {
		t.Fatal("no identity should not invent sticky id")
	}
}

func TestBuildRunPayloadUsesStickyConversationID(t *testing.T) {
	sticky := StickyConversationID(ConversationIdentity{SessionID: "sess-42"})
	if sticky == "" {
		t.Fatal("expected sticky id")
	}
	p1, err := BuildRunPayloadSelection(LiteralModelSelection("composer-2"), ParsedChat{
		UserText:             "hi",
		StickyConversationID: sticky,
	})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := BuildRunPayloadSelection(LiteralModelSelection("composer-2"), ParsedChat{
		UserText:             "again",
		StickyConversationID: sticky,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Conversation != sticky || p2.Conversation != sticky {
		t.Fatalf("payload conversation = %q / %q, want %q", p1.Conversation, p2.Conversation, sticky)
	}
	p3, err := BuildRunPayloadSelection(LiteralModelSelection("composer-2"), ParsedChat{UserText: "rand"})
	if err != nil {
		t.Fatal(err)
	}
	if p3.Conversation == "" || p3.Conversation == sticky {
		t.Fatalf("missing sticky should use random distinct id, got %q", p3.Conversation)
	}
}

func TestDeriveBridgeKeyWithIdentity(t *testing.T) {
	msgs := []ChatMessage{{Role: "user", Content: "hello"}}
	id := ConversationIdentity{ConversationID: "bridge-1"}
	a := DeriveBridgeKeyWithIdentity("m", msgs, id)
	b := DeriveBridgeKeyWithIdentity("m", msgs, id)
	if a == "" || a != b {
		t.Fatalf("bridge keys %q vs %q", a, b)
	}
	fallback := DeriveBridgeKeyWithIdentity("m", msgs, ConversationIdentity{})
	if fallback != DeriveBridgeKey("m", msgs) {
		t.Fatalf("empty identity should match DeriveBridgeKey: %q vs %q", fallback, DeriveBridgeKey("m", msgs))
	}
	if DeriveBridgeKeyWithIdentity("m", msgs, ConversationIdentity{ConversationID: "other"}) == a {
		t.Fatal("different identity should change bridge key")
	}
}
