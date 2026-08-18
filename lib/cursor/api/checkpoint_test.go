package cursor_api_sdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

func TestCheckpointStorePutGet(t *testing.T) {
	store := NewCheckpointStore(time.Minute)
	state := &cursorProto.ConversationStateStructure{
		RootPromptMessagesJson: [][]byte{[]byte("sys")},
		Todos:                  [][]byte{[]byte("todo-1")},
		ReadPaths:              []string{"/tmp/a"},
	}
	store.Put("k1", state, map[string][]byte{"aa": []byte("blob-a")})

	got := store.Get("k1")
	if got == nil || got.State == nil {
		t.Fatal("expected stored checkpoint")
	}
	if !bytes.Equal(got.State.Todos[0], []byte("todo-1")) {
		t.Fatalf("todos = %#v", got.State.Todos)
	}
	if string(got.Blobs["aa"]) != "blob-a" {
		t.Fatalf("blobs = %#v", got.Blobs)
	}
	// Mutations on returned copy must not affect store.
	got.State.Todos[0][0] = 'X'
	got.Blobs["aa"][0] = 'Z'
	again := store.Get("k1")
	if string(again.State.Todos[0]) != "todo-1" || string(again.Blobs["aa"]) != "blob-a" {
		t.Fatal("Get must return deep copies")
	}
}

func TestCheckpointStoreEmptyKeyNoop(t *testing.T) {
	store := NewCheckpointStore(time.Minute)
	store.Put("", &cursorProto.ConversationStateStructure{Todos: [][]byte{[]byte("x")}}, nil)
	if store.Get("") != nil {
		t.Fatal("empty key must not store")
	}
}

func TestCheckpointStoreMergeLiveBlobs(t *testing.T) {
	store := NewCheckpointStore(time.Minute)
	store.Put("k", &cursorProto.ConversationStateStructure{}, map[string][]byte{"a": []byte("1")})
	store.Put("k", &cursorProto.ConversationStateStructure{ReadPaths: []string{"p"}}, map[string][]byte{"b": []byte("2")})
	got := store.Get("k")
	if got == nil {
		t.Fatal("miss")
	}
	if string(got.Blobs["a"]) != "1" || string(got.Blobs["b"]) != "2" {
		t.Fatalf("merged blobs = %#v", got.Blobs)
	}
	if len(got.State.ReadPaths) != 1 || got.State.ReadPaths[0] != "p" {
		t.Fatalf("state = %#v", got.State)
	}
}

func TestCheckpointStoreExpiry(t *testing.T) {
	store := NewCheckpointStore(5 * time.Millisecond)
	store.Put("k", &cursorProto.ConversationStateStructure{Todos: [][]byte{[]byte("t")}}, nil)
	time.Sleep(20 * time.Millisecond)
	if store.Get("k") != nil {
		t.Fatal("expected expiry")
	}
}

func TestSystemPromptCompatibleTextFallback(t *testing.T) {
	sysJSON := []byte(`{"role":"system","content":"be brief"}`)
	sum := sha256.Sum256(sysJSON)
	serverID := append([]byte(nil), sum[:]...)
	blobs := map[string][]byte{hex.EncodeToString(serverID): sysJSON}
	state := &cursorProto.ConversationStateStructure{
		RootPromptMessagesJson: [][]byte{serverID},
		Todos:                  [][]byte{[]byte("todo")},
	}
	ourID := []byte("not-the-same-id-0123456789abcdef!!") // length irrelevant for text path
	if !SystemPromptCompatible(state, [][]byte{ourID}, blobs, "be brief") {
		t.Fatal("expected text-fallback match")
	}
	if SystemPromptCompatible(state, [][]byte{ourID}, blobs, "different") {
		t.Fatal("expected mismatch on different system text")
	}
}

func TestSystemPromptMatches(t *testing.T) {
	sys := []byte("sys-id")
	state := &cursorProto.ConversationStateStructure{
		RootPromptMessagesJson: [][]byte{sys, []byte("user")},
	}
	if !SystemPromptMatches(state, [][]byte{sys}) {
		t.Fatal("expected match")
	}
	if SystemPromptMatches(state, [][]byte{[]byte("other")}) {
		t.Fatal("expected mismatch")
	}
	if SystemPromptMatches(nil, [][]byte{sys}) {
		t.Fatal("nil checkpoint")
	}
	if SystemPromptMatches(state, nil) {
		t.Fatal("empty system ids")
	}
}

func TestMergeCheckpointStatePreservesNonHistory(t *testing.T) {
	prior := &cursorProto.ConversationStateStructure{
		RootPromptMessagesJson: [][]byte{[]byte("old-sys")},
		Turns:                  [][]byte{[]byte("old-turn")},
		Todos:                  [][]byte{[]byte("keep-todo")},
		ReadPaths:              []string{"/kept"},
		SelfSummaryCount:       3,
	}
	root := [][]byte{[]byte("new-sys"), []byte("new-user")}
	turns := [][]byte{[]byte("new-turn")}
	merged := MergeCheckpointState(prior, root, turns)
	if !bytes.Equal(merged.Todos[0], []byte("keep-todo")) {
		t.Fatalf("todos lost: %#v", merged.Todos)
	}
	if merged.SelfSummaryCount != 3 || merged.ReadPaths[0] != "/kept" {
		t.Fatalf("non-history lost: %#v", merged)
	}
	if !bytes.Equal(merged.RootPromptMessagesJson[0], []byte("new-sys")) {
		t.Fatalf("root not rebuilt: %#v", merged.RootPromptMessagesJson)
	}
	if !bytes.Equal(merged.Turns[0], []byte("new-turn")) {
		t.Fatalf("turns not rebuilt: %#v", merged.Turns)
	}
}

func TestRootJSONBlobFieldOrder(t *testing.T) {
	store := map[string][]byte{}
	id, err := storeJSONBlob(store, rootRoleMessage{Role: "system", Content: "be brief"})
	if err != nil {
		t.Fatal(err)
	}
	raw := store[hex.EncodeToString(id)]
	if !bytes.HasPrefix(raw, []byte(`{"role":"system","content":`)) {
		t.Fatalf("system json field order = %s", raw)
	}
	uid, err := storeJSONBlob(store, rootRoleMessage{
		Role:    "user",
		Content: []rootTextPart{{Type: "text", Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	uraw := store[hex.EncodeToString(uid)]
	want := `{"role":"user","content":[{"type":"text","text":"hi"}]}`
	if string(uraw) != want {
		t.Fatalf("user json = %s, want %s", uraw, want)
	}
}

func TestBuildRunPayloadWithCheckpointModes(t *testing.T) {
	sel := LiteralModelSelection("composer-2")
	first, err := BuildRunPayloadWithCheckpoint(sel, ParsedChat{
		SystemPrompt: "be brief",
		UserText:     "hi",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.CheckpointMode != CheckpointMiss {
		t.Fatalf("mode = %q, want miss", first.CheckpointMode)
	}

	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(first.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	state := msg.GetRunRequest().GetConversationState()
	if state == nil || len(state.RootPromptMessagesJson) == 0 {
		t.Fatal("missing conversation state")
	}
	sysID := append([]byte(nil), state.RootPromptMessagesJson[0]...)

	// Hit: same system prompt blob head + non-history preserved.
	prior := &StoredCheckpoint{
		State: &cursorProto.ConversationStateStructure{
			RootPromptMessagesJson: [][]byte{sysID},
			Todos:                  [][]byte{[]byte("sticky-todo")},
			ReadPaths:              []string{"/from-checkpoint"},
		},
		Blobs: map[string][]byte{"deadbeef": []byte("prior-blob")},
	}
	hit, err := BuildRunPayloadWithCheckpoint(sel, ParsedChat{
		SystemPrompt: "be brief",
		UserText:     "again",
		Turns:        []ConversationTurn{{UserText: "hi", AssistantText: "hello"}},
	}, prior)
	if err != nil {
		t.Fatal(err)
	}
	if hit.CheckpointMode != CheckpointHit {
		t.Fatalf("mode = %q, want hit", hit.CheckpointMode)
	}
	if _, ok := hit.BlobStore["deadbeef"]; !ok {
		t.Fatal("prior blobs must be seeded on hit")
	}
	var hitMsg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(hit.RequestBytes, &hitMsg); err != nil {
		t.Fatal(err)
	}
	hitState := hitMsg.GetRunRequest().GetConversationState()
	if hitState == nil || len(hitState.Todos) != 1 || !bytes.Equal(hitState.Todos[0], []byte("sticky-todo")) {
		t.Fatalf("hit must merge todos: %#v", hitState)
	}
	if len(hitState.ReadPaths) != 1 || hitState.ReadPaths[0] != "/from-checkpoint" {
		t.Fatalf("hit must keep read paths: %#v", hitState.ReadPaths)
	}

	// Rebuild: system prompt changed → fresh non-history.
	rebuild, err := BuildRunPayloadWithCheckpoint(sel, ParsedChat{
		SystemPrompt: "be verbose now",
		UserText:     "again",
	}, prior)
	if err != nil {
		t.Fatal(err)
	}
	if rebuild.CheckpointMode != CheckpointRebuild {
		t.Fatalf("mode = %q, want rebuild", rebuild.CheckpointMode)
	}
	var rebuildMsg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(rebuild.RequestBytes, &rebuildMsg); err != nil {
		t.Fatal(err)
	}
	rebuildState := rebuildMsg.GetRunRequest().GetConversationState()
	if len(rebuildState.GetTodos()) != 0 {
		t.Fatalf("rebuild must drop prior todos: %#v", rebuildState.Todos)
	}
}
