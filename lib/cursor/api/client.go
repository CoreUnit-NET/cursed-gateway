package cursor_api_sdk

/*
Package cursor_api_sdk talks to Cursor Connect / JSON HTTP APIs.

OAuth poll/refresh lives in lib/cursor/account. This package owns
AvailableModels, GetServerConfig, and AgentService/Run streaming.
*/

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAPIBaseURL   = "https://api2.cursor.sh"
	DefaultClientType   = "cli"
	fallbackClientVer   = "cli-2026.07.16-899851b"
	connectProtoVersion = "1"

	pathAvailableModels = "/aiserver.v1.AiService/AvailableModels"
	pathServerConfig    = "/aiserver.v1.ServerConfigService/GetServerConfig"
	pathAgentRun        = "/agent.v1.AgentService/Run"

	unaryTimeout   = 5 * time.Second
	runIdleTime    = 120 * time.Second
	heartbeatEvery = 5 * time.Second
)

// Client is a Cursor upstream API client. Safe for concurrent use.
type Client struct {
	HTTP          *http.Client
	BaseURL       string // default api2.cursor.sh
	ClientVersion string // e.g. cli-YYYY.MM.DD-hash
	Device        DeviceIDs

	mu         sync.Mutex
	agentURL   string // memoized GetServerConfig origin
	models     map[string]Model
	modelsList []Model
	modelsAt   time.Time
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 0} // streaming Run has its own idle limits
}

func (c *Client) baseURL() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultAPIBaseURL
}

func (c *Client) clientVersion() string {
	if c != nil && strings.TrimSpace(c.ClientVersion) != "" {
		return c.ClientVersion
	}
	if v := strings.TrimSpace(os.Getenv("CURSOR_CLIENT_VERSION")); v != "" {
		return v
	}
	if v := detectLocalAgentClientVersion(); v != "" {
		return v
	}
	return fallbackClientVer
}

func (c *Client) deviceIDs() DeviceIDs {
	if c != nil && c.Device.MachineID != "" {
		return c.Device
	}
	return GetDeviceIDs()
}

func detectLocalAgentClientVersion() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	versions := filepath.Join(home, ".local", "share", "cursor-agent", "versions")
	entries, err := os.ReadDir(versions)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best = e.Name()
			bestTime = info.ModTime()
		}
	}
	if best == "" {
		return ""
	}
	ver := "cli-" + best
	if !validClientVersion(ver) {
		return ""
	}
	return ver
}

func validClientVersion(v string) bool {
	if !strings.HasPrefix(v, "cli-") || len(v) < 5 {
		return false
	}
	for _, r := range v[4:] {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '.' || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (c *Client) setCommonHeaders(h http.Header, accessToken string) {
	ids := c.deviceIDs()
	h.Set("authorization", "Bearer "+accessToken)
	h.Set("connect-protocol-version", connectProtoVersion)
	h.Set("x-cursor-client-type", DefaultClientType)
	h.Set("x-cursor-client-version", c.clientVersion())
	h.Set("x-cursor-checksum", ChecksumHeader(ids, time.Now()))
	h.Set("x-ghost-mode", "true")
	h.Set("x-request-id", newRequestID())
}
