package cursor_account_sdk

/*
Package cursor_account_sdk is a reusable multi-account Cursor library.

Provide a single-account struct and a multi-account container (map of
accounts) for login, refresh, and usage helpers — OOP-style via structs.

No process/goroutine management here; callers (login_session) own that.
*/

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultLoginURL         = "https://cursor.com/loginDeepControl"
	DefaultPollURL          = "https://api2.cursor.sh/auth/poll"
	DefaultRefreshURL       = "https://api2.cursor.sh/auth/exchange_user_api_key"
	DefaultStripeProfileURL = "https://api2.cursor.sh/auth/full_stripe_profile"

	DefaultPollMaxAttempts = 150
	DefaultPollBaseDelay   = time.Second
	DefaultPollMaxDelay    = 10 * time.Second
	pollBackoffMultiplier  = 1.2

	// AccessExpiryMargin is subtracted from JWT exp when computing ExpiresAt.
	AccessExpiryMargin = 5 * time.Minute

	TierUnknown = "unknown"
)

var (
	ErrPollTimeout         = errors.New("cursor authentication polling timeout")
	ErrTooManyPollErrors   = errors.New("too many consecutive errors during cursor auth polling")
	ErrRefreshRejected     = errors.New("cursor token refresh rejected")
	ErrRefreshTransient    = errors.New("cursor token refresh failed")
	ErrMissingRefreshToken = errors.New("missing refresh token")
	ErrMissingAccessToken  = errors.New("missing access token")
)

// AuthParams is the PKCE login session handed to the browser + poller.
type AuthParams struct {
	Verifier  string
	Challenge string
	UUID      string
	LoginURL  string
}

// Credentials is an OAuth access/refresh pair with a local expiry hint.
type Credentials struct {
	Access  string
	Refresh string
	// ExpiresAt is when the access token should be treated as expired (unix ms),
	// already reduced by AccessExpiryMargin when parsed from JWT exp.
	ExpiresAt int64
}

// Account is one Cursor login identity plus optional metadata.
type Account struct {
	ID            string
	Label         string
	Subject       string
	Tier          string
	Access        string
	Refresh       string
	ExpiresAt     int64
	LastRefreshAt int64
	CreatedAt     int64
	UpdatedAt     int64
}

// CredentialsFromAccount copies token fields.
func (a *Account) Credentials() Credentials {
	if a == nil {
		return Credentials{}
	}
	return Credentials{
		Access:    a.Access,
		Refresh:   a.Refresh,
		ExpiresAt: a.ExpiresAt,
	}
}

// NeedsRefresh reports whether access should be refreshed now.
func (a *Account) NeedsRefresh(now time.Time) bool {
	if a == nil || a.Refresh == "" {
		return false
	}
	if a.Access == "" {
		return true
	}
	if a.ExpiresAt <= 0 {
		return true
	}
	return now.UnixMilli() >= a.ExpiresAt
}

// ApplyCredentials updates token fields and timestamps.
func (a *Account) ApplyCredentials(creds Credentials, now time.Time) {
	if a == nil {
		return
	}
	ms := now.UnixMilli()
	a.Access = creds.Access
	if creds.Refresh != "" {
		a.Refresh = creds.Refresh
	}
	a.ExpiresAt = creds.ExpiresAt
	a.LastRefreshAt = ms
	a.UpdatedAt = ms
	if sub, ok := JWTSubject(creds.Access); ok {
		a.Subject = sub
	}
}

// Endpoints holds overridable Cursor auth URLs (tests / custom upstream).
type Endpoints struct {
	LoginURL         string
	PollURL          string
	RefreshURL       string
	StripeProfileURL string
}

func (e Endpoints) withDefaults() Endpoints {
	if e.LoginURL == "" {
		e.LoginURL = DefaultLoginURL
	}
	if e.PollURL == "" {
		e.PollURL = DefaultPollURL
	}
	if e.RefreshURL == "" {
		e.RefreshURL = DefaultRefreshURL
	}
	if e.StripeProfileURL == "" {
		e.StripeProfileURL = DefaultStripeProfileURL
	}
	return e
}

// Client talks to Cursor auth HTTP endpoints. Safe for concurrent use.
type Client struct {
	HTTP      *http.Client
	Endpoints Endpoints
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) endpoints() Endpoints {
	if c == nil {
		return Endpoints{}.withDefaults()
	}
	return c.Endpoints.withDefaults()
}

// GeneratePKCE returns a verifier and S256 challenge (base64url, no padding).
func GeneratePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, 96)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("pkce random: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// GenerateAuthParams builds the Deep Control login URL for a new PKCE session.
func (c *Client) GenerateAuthParams() (AuthParams, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return AuthParams{}, err
	}
	id, err := randomUUID()
	if err != nil {
		return AuthParams{}, err
	}
	ep := c.endpoints()
	q := url.Values{}
	q.Set("challenge", challenge)
	q.Set("uuid", id)
	q.Set("mode", "login")
	q.Set("redirectTarget", "cli")
	return AuthParams{
		Verifier:  verifier,
		Challenge: challenge,
		UUID:      id,
		LoginURL:  ep.LoginURL + "?" + q.Encode(),
	}, nil
}

type pollTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// TryPollAuth performs one poll attempt. ok=false means still waiting (HTTP 404).
func (c *Client) TryPollAuth(ctx context.Context, uuid, verifier string) (Credentials, bool, error) {
	ep := c.endpoints()
	u, err := url.Parse(ep.PollURL)
	if err != nil {
		return Credentials{}, false, err
	}
	q := u.Query()
	q.Set("uuid", uuid)
	q.Set("verifier", verifier)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Credentials{}, false, err
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return Credentials{}, false, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode == http.StatusNotFound {
		return Credentials{}, false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Credentials{}, false, fmt.Errorf("poll failed: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokens pollTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return Credentials{}, false, fmt.Errorf("poll decode: %w", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return Credentials{}, false, fmt.Errorf("poll response missing tokens")
	}
	return Credentials{
		Access:    tokens.AccessToken,
		Refresh:   tokens.RefreshToken,
		ExpiresAt: TokenExpiryMilli(tokens.AccessToken, time.Now()),
	}, true, nil
}

// PollAuth waits until the user completes browser login or the attempt budget is exhausted.
func (c *Client) PollAuth(ctx context.Context, uuid, verifier string) (Credentials, error) {
	delay := DefaultPollBaseDelay
	consecutiveErrors := 0

	for attempt := 0; attempt < DefaultPollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		case <-time.After(delay):
		}

		creds, ok, err := c.TryPollAuth(ctx, uuid, verifier)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= 3 {
				return Credentials{}, fmt.Errorf("%w: %v", ErrTooManyPollErrors, err)
			}
			continue
		}
		consecutiveErrors = 0
		if ok {
			return creds, nil
		}
		next := time.Duration(float64(delay) * pollBackoffMultiplier)
		if next > DefaultPollMaxDelay {
			next = DefaultPollMaxDelay
		}
		delay = next
	}
	return Credentials{}, ErrPollTimeout
}

type refreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type stripeProfileResponse struct {
	MembershipType    string `json:"membershipType"`
	MembershipTypeAlt string `json:"membership_type"`
}

// NormalizeTier lowercases and canonicalizes Cursor membership strings.
// Empty input becomes TierUnknown.
func NormalizeTier(tier string) string {
	t := strings.ToLower(strings.TrimSpace(tier))
	switch t {
	case "":
		return TierUnknown
	case "pro+", "proplus":
		return "pro_plus"
	default:
		return t
	}
}

// TierKnown reports whether tier is a non-empty, non-placeholder membership.
func TierKnown(tier string) bool {
	t := NormalizeTier(tier)
	return t != "" && t != TierUnknown
}

// FetchTier loads membershipType from Cursor's full stripe profile.
func (c *Client) FetchTier(ctx context.Context, accessToken string) (string, error) {
	if accessToken == "" {
		return "", ErrMissingAccessToken
	}
	ep := c.endpoints()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.StripeProfileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("stripe profile failed: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var data stripeProfileResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("stripe profile decode: %w", err)
	}
	tier := data.MembershipType
	if tier == "" {
		tier = data.MembershipTypeAlt
	}
	tier = NormalizeTier(tier)
	if !TierKnown(tier) {
		return TierUnknown, fmt.Errorf("stripe profile missing membershipType")
	}
	return tier, nil
}

// RefreshToken exchanges a refresh JWT for a new access (and optionally refresh) token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (Credentials, error) {
	if refreshToken == "" {
		return Credentials{}, ErrMissingRefreshToken
	}
	ep := c.endpoints()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.RefreshURL, strings.NewReader("{}"))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("%w: %v", ErrRefreshTransient, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		permanent := res.StatusCode >= 400 && res.StatusCode < 500 &&
			res.StatusCode != http.StatusRequestTimeout &&
			res.StatusCode != http.StatusTooManyRequests
		msg := strings.TrimSpace(string(body))
		if permanent {
			return Credentials{}, fmt.Errorf("%w: HTTP %d: %s", ErrRefreshRejected, res.StatusCode, msg)
		}
		return Credentials{}, fmt.Errorf("%w: HTTP %d: %s", ErrRefreshTransient, res.StatusCode, msg)
	}

	var data refreshResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return Credentials{}, fmt.Errorf("refresh decode: %w", err)
	}
	if data.AccessToken == "" {
		return Credentials{}, ErrMissingAccessToken
	}
	nextRefresh := refreshToken
	if LooksLikeJWT(data.RefreshToken) {
		nextRefresh = data.RefreshToken
	}
	return Credentials{
		Access:    data.AccessToken,
		Refresh:   nextRefresh,
		ExpiresAt: TokenExpiryMilli(data.AccessToken, time.Now()),
	}, nil
}

// Login runs GenerateAuthParams, invokes onLoginURL (e.g. print/open browser), then PollAuth.
func (c *Client) Login(ctx context.Context, onLoginURL func(loginURL string) error) (Credentials, AuthParams, error) {
	params, err := c.GenerateAuthParams()
	if err != nil {
		return Credentials{}, AuthParams{}, err
	}
	if onLoginURL != nil {
		if err := onLoginURL(params.LoginURL); err != nil {
			return Credentials{}, params, err
		}
	}
	creds, err := c.PollAuth(ctx, params.UUID, params.Verifier)
	return creds, params, err
}

// TokenExpiryMilli reads JWT exp and returns unix-ms minus AccessExpiryMargin.
// Falls back to now+1h − margin when the token cannot be parsed.
func TokenExpiryMilli(token string, now time.Time) int64 {
	if exp, ok := jwtExpiryUnix(token); ok {
		return exp*1000 - AccessExpiryMargin.Milliseconds()
	}
	return now.Add(time.Hour).UnixMilli() - AccessExpiryMargin.Milliseconds()
}

// LooksLikeJWT reports a three-segment non-empty dotted token.
func LooksLikeJWT(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// JWTSubject extracts the optional "sub" claim from a JWT payload.
func JWTSubject(token string) (string, bool) {
	claims, ok := jwtClaims(token)
	if !ok {
		return "", false
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", false
	}
	return sub, true
}

func jwtExpiryUnix(token string) (int64, bool) {
	claims, ok := jwtClaims(token)
	if !ok {
		return 0, false
	}
	switch v := claims["exp"].(type) {
	case float64:
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func jwtClaims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens use padded std encoding; try URLEncoding with padding trim.
		payload, err = base64.URLEncoding.DecodeString(padBase64(parts[1]))
		if err != nil {
			return nil, false
		}
	}
	var claims map[string]any
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil {
		return nil, false
	}
	return claims, true
}

func padBase64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	default:
		return s
	}
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("uuid random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// NewAccountFromCredentials builds an Account with a fresh id and timestamps.
func NewAccountFromCredentials(creds Credentials, now time.Time) (*Account, error) {
	id, err := randomUUID()
	if err != nil {
		return nil, err
	}
	ms := now.UnixMilli()
	a := &Account{
		ID:            id,
		Access:        creds.Access,
		Refresh:       creds.Refresh,
		ExpiresAt:     creds.ExpiresAt,
		LastRefreshAt: ms,
		CreatedAt:     ms,
		UpdatedAt:     ms,
		Tier:          TierUnknown,
	}
	if sub, ok := JWTSubject(creds.Access); ok {
		a.Subject = sub
	}
	return a, nil
}
