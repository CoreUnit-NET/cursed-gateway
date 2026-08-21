package login_session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestNewStoreCreatesEmptyFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "data.json")
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Fatalf("stat before: %v", err)
	}

	store, err := NewStore(storePath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("len(List)=%d, want 0", len(store.List()))
	}

	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed StoreFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, raw)
	}
	if parsed.Sessions == nil {
		t.Fatal("sessions is null, want []")
	}
	if len(parsed.Sessions) != 0 {
		t.Fatalf("len(sessions)=%d, want 0", len(parsed.Sessions))
	}
}

func TestNewStoreReadsExistingThenRewrites(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(storePath, []byte(`{"sessions":[{"id":"keep-me","access":"a","refresh":"r","expires":1}]}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, err := NewStore(storePath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := store.List(); len(got) != 1 || got[0].ID != "keep-me" {
		t.Fatalf("List=%v, want keep-me", got)
	}

	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed StoreFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, raw)
	}
	if len(parsed.Sessions) != 1 || parsed.Sessions[0].ID != "keep-me" {
		t.Fatalf("persisted=%+v, want keep-me", parsed.Sessions)
	}
}

func TestNewStoreFailsUnwritableDir(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	existing := filepath.Join(locked, "existing.json")
	if err := os.WriteFile(existing, []byte(`{"sessions":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	for _, name := range []string{"missing.json", "existing.json"} {
		_, err := NewStore(filepath.Join(locked, name), &cursor_account_sdk.Client{})
		if err == nil {
			t.Fatalf("%s: NewStore succeeded, want write error", name)
		}
		if !strings.Contains(err.Error(), "write auth store tmp") {
			t.Fatalf("%s: err=%v, want write auth store tmp", name, err)
		}
	}
}

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

func TestStoreFindRemoveMatchAndPublicID(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	acc := &cursor_account_sdk.Account{
		ID:      "store-uuid",
		Subject: "user_find",
		Tier:    "pro",
		Access:  "tok",
		Refresh: "ref",
	}
	if err := store.Add(acc); err != nil {
		t.Fatalf("Add: %v", err)
	}

	byID, err := store.Find("store-uuid")
	if err != nil || byID.Subject != "user_find" {
		t.Fatalf("Find id: acc=%+v err=%v", byID, err)
	}
	bySub, err := store.Find("user_find")
	if err != nil || bySub.ID != "store-uuid" {
		t.Fatalf("Find subject: acc=%+v err=%v", bySub, err)
	}
	if _, err := store.Find(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find empty err=%v", err)
	}
	if PublicAccountID(acc) != "user_find" {
		t.Fatalf("PublicAccountID=%q", PublicAccountID(acc))
	}
	if PublicAccountID(&cursor_account_sdk.Account{ID: "only-store"}) != "only-store" {
		t.Fatal("expected store id fallback")
	}

	n, err := store.RemoveMatch("user_find")
	if err != nil || n != 1 {
		t.Fatalf("RemoveMatch: n=%d err=%v", n, err)
	}
	if _, err := store.Find("user_find"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find after remove err=%v", err)
	}
}

func TestTestAndStoreRefreshThenUpsert(t *testing.T) {
	access := fakeJWT(t, "user_test", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "user_test", time.Now().Add(24*time.Hour))
	refreshSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  access,
			"refreshToken": refresh,
		})
	}))
	t.Cleanup(refreshSrv.Close)

	dir := t.TempDir()
	client := &cursor_account_sdk.Client{
		HTTP: refreshSrv.Client(),
		Endpoints: cursor_account_sdk.Endpoints{
			RefreshURL: refreshSrv.URL,
		},
	}
	store, err := NewStore(filepath.Join(dir, "data.json"), client)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	first, merged, err := store.TestAndStore(context.Background(), cursor_account_sdk.Credentials{
		Refresh: "refresh-token",
	})
	if err != nil {
		t.Fatalf("TestAndStore: %v", err)
	}
	if merged {
		t.Fatal("first store should not merge")
	}
	if PublicAccountID(first) != "user_test" {
		t.Fatalf("id=%q", PublicAccountID(first))
	}

	second, merged, err := store.TestAndStore(context.Background(), cursor_account_sdk.Credentials{
		Refresh: "refresh-token",
	})
	if err != nil {
		t.Fatalf("TestAndStore upsert: %v", err)
	}
	if !merged {
		t.Fatal("second store should merge")
	}
	if second.ID != first.ID {
		t.Fatalf("id changed %q -> %q", first.ID, second.ID)
	}
	if len(store.List()) != 1 {
		t.Fatalf("len=%d, want 1", len(store.List()))
	}

	if _, _, err := store.TestAndStore(context.Background(), cursor_account_sdk.Credentials{}); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("empty refresh err=%v", err)
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

func TestParseAuthPayload(t *testing.T) {
	refresh := fakeJWT(t, "user_payload", time.Now().Add(time.Hour))
	access := fakeJWT(t, "user_payload", time.Now().Add(time.Hour))

	creds, err := ParseAuthPayload([]byte(`{"refreshToken":"` + refresh + `","accessToken":"` + access + `"}`))
	if err != nil {
		t.Fatalf("ParseAuthPayload: %v", err)
	}
	if creds.Refresh != refresh || creds.Access != access {
		t.Fatalf("creds = %+v", creds)
	}

	nested, err := ParseAuthPayload([]byte(`{"cursor":{"refresh":"` + refresh + `"}}`))
	if err != nil {
		t.Fatalf("nested: %v", err)
	}
	if nested.Refresh != refresh {
		t.Fatalf("nested refresh mismatch")
	}

	if _, err := ParseAuthPayload([]byte(`{}`)); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("empty payload err=%v, want ErrInvalidImport", err)
	}
	if _, err := ParseAuthPayload([]byte(`{`)); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("invalid json err=%v, want ErrInvalidImport", err)
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
