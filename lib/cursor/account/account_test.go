package cursor_account_sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeTier(t *testing.T) {
	cases := map[string]string{
		"":        TierUnknown,
		"  PRO  ": "pro",
		"Pro+":    "pro_plus",
		"proplus": "pro_plus",
		"free":    "free",
	}
	for in, want := range cases {
		if got := NormalizeTier(in); got != want {
			t.Fatalf("NormalizeTier(%q)=%q, want %q", in, got, want)
		}
	}
	if TierKnown(TierUnknown) || TierKnown("") {
		t.Fatal("placeholder tiers must not be known")
	}
	if !TierKnown("pro") {
		t.Fatal("pro should be known")
	}
}

func TestFetchTier(t *testing.T) {
	t.Run("membershipType Pro+", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/auth/full_stripe_profile" {
				http.NotFound(w, r)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
				t.Errorf("Authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"membershipType":"Pro+"}`))
		}))
		t.Cleanup(srv.Close)

		c := &Client{
			HTTP: srv.Client(),
			Endpoints: Endpoints{
				StripeProfileURL: srv.URL + "/auth/full_stripe_profile",
			},
		}
		tier, err := c.FetchTier(context.Background(), "access-token")
		if err != nil {
			t.Fatal(err)
		}
		if tier != "pro_plus" {
			t.Fatalf("tier=%q, want pro_plus", tier)
		}
	})

	t.Run("membership_type fallback", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"membership_type":"business"}`))
		}))
		t.Cleanup(srv.Close)
		c := &Client{
			HTTP:      srv.Client(),
			Endpoints: Endpoints{StripeProfileURL: srv.URL},
		}
		tier, err := c.FetchTier(context.Background(), "tok")
		if err != nil {
			t.Fatal(err)
		}
		if tier != "business" {
			t.Fatalf("tier=%q, want business", tier)
		}
	})

	t.Run("http error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		}))
		t.Cleanup(srv.Close)
		c := &Client{
			HTTP:      srv.Client(),
			Endpoints: Endpoints{StripeProfileURL: srv.URL},
		}
		if _, err := c.FetchTier(context.Background(), "tok"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing membership", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)
		c := &Client{
			HTTP:      srv.Client(),
			Endpoints: Endpoints{StripeProfileURL: srv.URL},
		}
		tier, err := c.FetchTier(context.Background(), "tok")
		if err == nil {
			t.Fatal("expected error")
		}
		if tier != TierUnknown {
			t.Fatalf("tier=%q, want %q", tier, TierUnknown)
		}
	})

	if _, err := (&Client{}).FetchTier(context.Background(), ""); !errors.Is(err, ErrMissingAccessToken) {
		t.Fatalf("empty access err=%v", err)
	}
}

func TestGenerateAuthParams(t *testing.T) {
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
