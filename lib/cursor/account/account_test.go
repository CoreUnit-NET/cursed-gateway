package cursor_account_sdk

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestGeneratePKCEAndAuthParams(t *testing.T) {
	c := &Client{}
	params, err := c.GenerateAuthParams()
	if err != nil {
		t.Fatal(err)
	}
	if params.Verifier == "" || params.Challenge == "" || params.UUID == "" {
		t.Fatalf("missing pkce fields: %+v", params)
	}
	if params.LoginURL == "" {
		t.Fatal("empty login url")
	}
}

func TestJWTSubjectAndExpiry(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	token := fakeJWT(t, "sub_123", exp)

	sub, ok := JWTSubject(token)
	if !ok || sub != "sub_123" {
		t.Fatalf("JWTSubject = %q ok=%v", sub, ok)
	}
	got := TokenExpiryMilli(token, time.Now())
	want := exp.Unix()*1000 - AccessExpiryMargin.Milliseconds()
	if got != want {
		t.Fatalf("TokenExpiryMilli = %d, want %d", got, want)
	}
}

func TestAccountNeedsRefresh(t *testing.T) {
	now := time.Now()
	a := &Account{
		Access:    "x",
		Refresh:   "y",
		ExpiresAt: now.Add(-time.Minute).UnixMilli(),
	}
	if !a.NeedsRefresh(now) {
		t.Fatal("expected needs refresh")
	}
	a.ExpiresAt = now.Add(time.Hour).UnixMilli()
	if a.NeedsRefresh(now) {
		t.Fatal("expected no refresh")
	}
}

func fakeJWT(t *testing.T, sub string, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{"sub": sub, "exp": exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}
