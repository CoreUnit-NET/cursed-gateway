package cursor_api_sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ModelParameter is a Cursor RequestedModel parameter (context, reasoning, …).
type ModelParameter struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// Model is a Cursor picker catalog entry mapped for OpenAI /v1/models.
type Model struct {
	ID               string           `json:"id"`
	Name             string           `json:"name,omitempty"`
	ServerModelName  string           `json:"server_model_name,omitempty"`
	LegacySlug       string           `json:"legacy_slug,omitempty"`
	Aliases          []string         `json:"aliases,omitempty"`
	SupportsThinking bool             `json:"supports_thinking,omitempty"`
	SupportsAgent    *bool            `json:"supports_agent,omitempty"`
	MaxMode          bool             `json:"max_mode,omitempty"`
	Parameters       []ModelParameter `json:"parameters,omitempty"`
}

// ModelSelection is the wire identity used for AgentService/Run.
type ModelSelection struct {
	PublicID      string
	WireModelID   string
	DisplayName   string
	Parameters    []ModelParameter
	MaxMode       bool
	SupportsAgent *bool
}

var fallbackModels = []Model{
	{ID: "default", Name: "Auto", ServerModelName: "default"},
	{ID: "composer-2.5", Name: "Composer 2.5", ServerModelName: "composer-2.5"},
	{ID: "cursor-small", Name: "Cursor Small", ServerModelName: "cursor-small"},
}

const modelCacheTTL = 5 * time.Minute

// ResolveModelID maps client-facing aliases to Cursor wire ids.
func ResolveModelID(modelID string) string {
	trimmed := strings.TrimSpace(modelID)
	if strings.EqualFold(trimmed, "auto") || trimmed == "" {
		return "default"
	}
	return trimmed
}

// SelectionFromModel builds a Run selection from a catalog entry.
// AgentService/Run validates ModelDetails.model_id against legacy slugs for some
// vendors (e.g. Anthropic), so WireModelID prefers variant legacySlug.
func SelectionFromModel(m Model) ModelSelection {
	publicID := ResolveModelID(m.ID)
	wire := strings.TrimSpace(m.LegacySlug)
	if wire == "" {
		wire = strings.TrimSpace(m.ServerModelName)
	}
	if wire == "" {
		wire = publicID
	}
	display := strings.TrimSpace(m.Name)
	if display == "" {
		display = publicID
	}
	params := append([]ModelParameter(nil), m.Parameters...)
	return ModelSelection{
		PublicID:      publicID,
		WireModelID:   wire,
		DisplayName:   display,
		Parameters:    params,
		MaxMode:       m.MaxMode,
		SupportsAgent: m.SupportsAgent,
	}
}

// LiteralModelSelection is used when the catalog has no entry for modelID.
func LiteralModelSelection(modelID string) ModelSelection {
	id := ResolveModelID(modelID)
	return ModelSelection{
		PublicID:    id,
		WireModelID: id,
		DisplayName: id,
	}
}

// CachedModels returns a copy of the in-process catalog when the TTL is still valid.
func (c *Client) CachedModels() []Model {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.modelsList) == 0 || time.Since(c.modelsAt) > modelCacheTTL {
		return nil
	}
	out := make([]Model, len(c.modelsList))
	copy(out, c.modelsList)
	return out
}

// ListModels calls AiService/AvailableModels; on failure returns a small fallback list.
func (c *Client) ListModels(ctx context.Context, accessToken string) ([]Model, error) {
	if cached := c.CachedModels(); len(cached) > 0 {
		return cached, nil
	}
	models, err := c.fetchAvailableModels(ctx, accessToken)
	if err != nil || len(models) == 0 {
		if err != nil {
			slog.Warn("AvailableModels failed; using fallback catalog",
				"err", err,
				"fallback", len(fallbackModels),
			)
		} else {
			slog.Warn("AvailableModels empty; using fallback catalog",
				"fallback", len(fallbackModels),
			)
		}
		out := make([]Model, len(fallbackModels))
		copy(out, fallbackModels)
		c.storeModelCache(out)
		return out, nil
	}
	c.storeModelCache(models)
	return models, nil
}

// ResolveModelSelection maps an OpenAI model id onto Cursor ModelDetails/RequestedModel fields.
func (c *Client) ResolveModelSelection(ctx context.Context, accessToken, modelID string) (ModelSelection, error) {
	publicID := ResolveModelID(modelID)
	if m, ok := c.lookupCachedModel(publicID); ok {
		sel := SelectionFromModel(m)
		slog.Debug("resolved model selection from cache",
			"public_id", sel.PublicID,
			"wire_model_id", sel.WireModelID,
			"display_name", sel.DisplayName,
			"max_mode", sel.MaxMode,
			"parameters", len(sel.Parameters),
			"supports_agent", supportsAgentLog(sel.SupportsAgent),
		)
		return sel, nil
	}

	models, err := c.ListModels(ctx, accessToken)
	if err != nil {
		return LiteralModelSelection(publicID), err
	}
	if m, ok := findModel(models, publicID); ok {
		sel := SelectionFromModel(m)
		slog.Debug("resolved model selection from catalog",
			"public_id", sel.PublicID,
			"wire_model_id", sel.WireModelID,
			"display_name", sel.DisplayName,
			"max_mode", sel.MaxMode,
			"parameters", len(sel.Parameters),
			"supports_agent", supportsAgentLog(sel.SupportsAgent),
			"server_model_name", m.ServerModelName,
			"legacy_slug", m.LegacySlug,
		)
		return sel, nil
	}

	sel := LiteralModelSelection(publicID)
	slog.Warn("model not present in AvailableModels; using literal ids",
		"public_id", sel.PublicID,
		"catalog_size", len(models),
	)
	return sel, nil
}

func (c *Client) storeModelCache(models []Model) {
	if c == nil {
		return
	}
	byID := make(map[string]Model, len(models))
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		byID[m.ID] = m
	}
	list := append([]Model(nil), models...)
	c.mu.Lock()
	c.models = byID
	c.modelsList = list
	c.modelsAt = time.Now()
	c.mu.Unlock()
}

func (c *Client) lookupCachedModel(id string) (Model, bool) {
	if c == nil {
		return Model{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.models == nil || time.Since(c.modelsAt) > modelCacheTTL {
		return Model{}, false
	}
	if m, ok := c.models[id]; ok {
		return m, true
	}
	for _, m := range c.models {
		if modelMatchesID(m, id) {
			return m, true
		}
	}
	return Model{}, false
}

func findModel(models []Model, id string) (Model, bool) {
	for _, m := range models {
		if modelMatchesID(m, id) {
			return m, true
		}
	}
	return Model{}, false
}

func modelMatchesID(m Model, id string) bool {
	if m.ID == id || m.ServerModelName == id || m.LegacySlug == id {
		return true
	}
	for _, a := range m.Aliases {
		if a == id {
			return true
		}
	}
	return false
}

func supportsAgentLog(v *bool) string {
	if v == nil {
		return "unknown"
	}
	if *v {
		return "true"
	}
	return "false"
}

// FormatModelParameters renders selection parameters as id=value pairs for logs/headers.
func FormatModelParameters(params []ModelParameter) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		parts = append(parts, id+"="+p.Value)
	}
	return strings.Join(parts, ",")
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
		m, ok := mapAvailableModel(rawModel)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func mapAvailableModel(rawModel json.RawMessage) (Model, bool) {
	var m struct {
		Name               string   `json:"name"`
		ServerModelName    string   `json:"serverModelName"`
		ServerModelName2   string   `json:"server_model_name"`
		ClientDisplayName  string   `json:"clientDisplayName"`
		ClientDisplayName2 string   `json:"client_display_name"`
		SupportsThinking   bool     `json:"supportsThinking"`
		SupportsThinking2  bool     `json:"supports_thinking"`
		SupportsAgent      *bool    `json:"supportsAgent"`
		SupportsAgent2     *bool    `json:"supports_agent"`
		SupportsMaxMode    bool     `json:"supportsMaxMode"`
		SupportsNonMaxMode bool     `json:"supportsNonMaxMode"`
		IDAliases          []string `json:"idAliases"`
		LegacySlugs        []string `json:"legacySlugs"`
		Variants           []struct {
			IsDefaultNonMaxConfig bool   `json:"isDefaultNonMaxConfig"`
			IsDefaultMaxConfig    bool   `json:"isDefaultMaxConfig"`
			IsMaxMode             bool   `json:"isMaxMode"`
			LegacySlug            string `json:"legacySlug"`
			ParameterValues       []struct {
				ID    string `json:"id"`
				Value string `json:"value"`
			} `json:"parameterValues"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(rawModel, &m); err != nil || m.Name == "" {
		return Model{}, false
	}

	display := m.ClientDisplayName
	if display == "" {
		display = m.ClientDisplayName2
	}
	serverName := strings.TrimSpace(m.ServerModelName)
	if serverName == "" {
		serverName = strings.TrimSpace(m.ServerModelName2)
	}
	if serverName == "" {
		serverName = m.Name
	}

	supportsAgent := m.SupportsAgent
	if supportsAgent == nil {
		supportsAgent = m.SupportsAgent2
	}

	params, maxMode, legacySlug := pickDefaultVariant(m.Variants)
	if legacySlug == "" && len(m.LegacySlugs) > 0 {
		legacySlug = m.LegacySlugs[0]
	}
	if len(m.Variants) == 0 && m.SupportsMaxMode && !m.SupportsNonMaxMode {
		maxMode = true
	}

	aliases := append([]string(nil), m.IDAliases...)
	for _, slug := range m.LegacySlugs {
		if slug == "" || slug == legacySlug {
			continue
		}
		aliases = append(aliases, slug)
	}

	return Model{
		ID:               m.Name,
		Name:             display,
		ServerModelName:  serverName,
		LegacySlug:       legacySlug,
		Aliases:          aliases,
		SupportsThinking: m.SupportsThinking || m.SupportsThinking2,
		SupportsAgent:    supportsAgent,
		MaxMode:          maxMode,
		Parameters:       params,
	}, true
}

func pickDefaultVariant(variants []struct {
	IsDefaultNonMaxConfig bool   `json:"isDefaultNonMaxConfig"`
	IsDefaultMaxConfig    bool   `json:"isDefaultMaxConfig"`
	IsMaxMode             bool   `json:"isMaxMode"`
	LegacySlug            string `json:"legacySlug"`
	ParameterValues       []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"parameterValues"`
}) ([]ModelParameter, bool, string) {
	if len(variants) == 0 {
		return nil, false, ""
	}
	chosen := variants[0]
	foundNonMax := false
	for _, v := range variants {
		if v.IsDefaultNonMaxConfig {
			chosen = v
			foundNonMax = true
			break
		}
	}
	if !foundNonMax {
		for _, v := range variants {
			if v.IsDefaultMaxConfig {
				chosen = v
				break
			}
		}
	}
	params := make([]ModelParameter, 0, len(chosen.ParameterValues))
	for _, p := range chosen.ParameterValues {
		if strings.TrimSpace(p.ID) == "" {
			continue
		}
		params = append(params, ModelParameter{ID: p.ID, Value: p.Value})
	}
	maxMode := chosen.IsMaxMode
	if !foundNonMax && chosen.IsDefaultMaxConfig {
		maxMode = true
	}
	return params, maxMode, strings.TrimSpace(chosen.LegacySlug)
}
