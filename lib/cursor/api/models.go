package cursor_api_sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Model is a Cursor picker catalog entry mapped for OpenAI /v1/models.
type Model struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	SupportsThinking bool   `json:"supports_thinking,omitempty"`
}

var fallbackModels = []Model{
	{ID: "default", Name: "Auto"},
	{ID: "composer-2.5", Name: "Composer 2.5"},
	{ID: "cursor-small", Name: "Cursor Small"},
}

// ResolveModelID maps client-facing aliases to Cursor wire ids.
func ResolveModelID(modelID string) string {
	trimmed := strings.TrimSpace(modelID)
	if strings.EqualFold(trimmed, "auto") || trimmed == "" {
		return "default"
	}
	return trimmed
}

// ListModels calls AiService/AvailableModels; on failure returns a small fallback list.
func (c *Client) ListModels(ctx context.Context, accessToken string) ([]Model, error) {
	models, err := c.fetchAvailableModels(ctx, accessToken)
	if err != nil || len(models) == 0 {
		out := make([]Model, len(fallbackModels))
		copy(out, fallbackModels)
		return out, nil
	}
	return models, nil
}

func (c *Client) fetchAvailableModels(ctx context.Context, accessToken string) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()

	body := []byte(`{"includeLongContextModels":true,"useModelParameters":true,"useCloudAgentEffortModes":true}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+pathAvailableModels, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req.Header, accessToken)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, classifyHTTP(res.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("AvailableModels decode: %w", err)
	}

	out := make([]Model, 0, len(parsed.Models))
	for _, rawModel := range parsed.Models {
		var m struct {
			Name               string `json:"name"`
			ClientDisplayName  string `json:"clientDisplayName"`
			ClientDisplayName2 string `json:"client_display_name"`
			SupportsThinking   bool   `json:"supportsThinking"`
			SupportsThinking2  bool   `json:"supports_thinking"`
		}
		if err := json.Unmarshal(rawModel, &m); err != nil || m.Name == "" {
			continue
		}
		display := m.ClientDisplayName
		if display == "" {
			display = m.ClientDisplayName2
		}
		out = append(out, Model{
			ID:               m.Name,
			Name:             display,
			SupportsThinking: m.SupportsThinking || m.SupportsThinking2,
		})
	}
	return out, nil
}
