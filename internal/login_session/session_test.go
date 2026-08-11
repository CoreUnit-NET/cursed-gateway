package login_session

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestStoreImportUpsertAndRemove(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "data.json")
	authPath := filepath.Join(dir, "auth.json")

	access := fakeJWT(t, "user_a", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "user_a", time.Now().Add(24*time.Hour))
	writeJSON(t, authPath, map[string]string{
		"accessToken":  access,
		"refreshToken": refresh,
	})

	store, err := NewStore(storePath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	acc, err := store.ImportAuthFile(authPath)
	if err != nil {
		t.Fatalf("ImportAuthFile: %v", err)
	}
	if acc.Subject != "user_a" {
		t.Fatalf("subject = %q, want user_a", acc.Subject)
	}
	if len(store.List()) != 1 {
		t.Fatalf("len(List)=%d, want 1", len(store.List()))
	}

	// Re-import same subject should upsert, not duplicate.
	access2 := fakeJWT(t, "user_a", time.Now().Add(2*time.Hour))
	writeJSON(t, authPath, map[string]string{
		"accessToken":  access2,
		"refreshToken": refresh,
	})
	acc2, err := store.ImportAuthFile(authPath)
	if err != nil {
		t.Fatalf("ImportAuthFile upsert: %v", err)
	}
	if acc2.ID != acc.ID {
		t.Fatalf("upsert changed id %q -> %q", acc.ID, acc2.ID)
	}
	if len(store.List()) != 1 {
		t.Fatalf("after upsert len=%d, want 1", len(store.List()))
	}

	// Nested OpenCode-style cursor key.
	nestedPath := filepath.Join(dir, "opencode-auth.json")
	writeJSON(t, nestedPath, map[string]any{
		"cursor": map[string]any{
			"type":    "oauth",
			"access":  fakeJWT(t, "user_b", time.Now().Add(time.Hour)),
			"refresh": fakeJWT(t, "user_b", time.Now().Add(24*time.Hour)),
			"expires": time.Now().Add(time.Hour).UnixMilli(),
		},
	})
	if _, err := store.ImportAuthFile(nestedPath); err != nil {
		t.Fatalf("nested import: %v", err)
	}
	if len(store.List()) != 2 {
		t.Fatalf("after nested import len=%d, want 2", len(store.List()))
	}

	n, err := store.Remove(acc.ID)
	if err != nil || n != 1 {
		t.Fatalf("Remove one: n=%d err=%v", n, err)
	}
	n, err = store.Remove("")
	if err != nil || n != 1 {
		t.Fatalf("Remove all: n=%d err=%v", n, err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty store")
	}
}

func TestParseImportRefreshOnly(t *testing.T) {
	refresh := fakeJWT(t, "user_c", time.Now().Add(time.Hour))
	creds, err := parseImportCredentials([]byte(`{"refreshToken":"` + refresh + `"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if creds.Refresh != refresh {
		t.Fatalf("refresh mismatch")
	}
	if creds.Access != "" {
		t.Fatalf("access want empty, got %q", creds.Access)
	}
}

func fakeJWT(t *testing.T, sub string, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{
		"sub": sub,
		"exp": exp.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
