package cursor_api_sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var cursorHostRE = regexp.MustCompile(`^([a-z0-9-]+\.)+cursor\.sh$`)

// ResolveAgentOrigin returns the HTTPS origin for AgentService/Run.
// Uses GetServerConfig when possible; falls back to BaseURL / api2.
func (c *Client) ResolveAgentOrigin(ctx context.Context, accessToken string) (string, error) {
	c.mu.Lock()
	cached := c.agentURL
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	origin, err := c.fetchAgentOrigin(ctx, accessToken)
	if err != nil || origin == "" {
		return c.baseURL(), nil
	}

	c.mu.Lock()
	c.agentURL = origin
	c.mu.Unlock()
	return origin, nil
}

func (c *Client) fetchAgentOrigin(ctx context.Context, accessToken string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()

	body := []byte(`{"telem_enabled":false}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+pathServerConfig, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.setCommonHeaders(req.Header, accessToken)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", classifyHTTP(res.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	candidate := ""
	if cfg, ok := parsed["agentUrlConfig"].(map[string]any); ok {
		if v, ok := cfg["agentnUrl"].(string); ok {
			candidate = v
		} else if v, ok := cfg["agentUrl"].(string); ok {
			candidate = v
		}
	}
	if candidate == "" {
		if v, ok := parsed["agentUrl"].(string); ok {
			candidate = v
		}
	}
	return normalizeAgentOrigin(candidate)
}

func normalizeAgentOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		u.Scheme = "https"
	}
	host := strings.ToLower(u.Hostname())
	if !cursorHostRE.MatchString(host) {
		return "", nil
	}
	return "https://" + host, nil
}
