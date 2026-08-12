package cursor_api_sdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogFixturesWireSelection(t *testing.T) {
	cases := []struct {
		file        string
		publicID    string
		wireID      string
		maxMode     bool
		paramID     string
		paramValue  string
		alias       string
		legacyMatch string
	}{
		{
			file:        "claude-haiku-4-5.json",
			publicID:    "claude-haiku-4-5",
			wireID:      "claude-4.5-haiku-thinking",
			paramID:     "thinking",
			paramValue:  "true",
			alias:       "haiku",
			legacyMatch: "claude-4.5-haiku",
		},
		{
			file:       "gemini-3.6-flash.json",
			publicID:   "gemini-3.6-flash",
			wireID:     "gemini-3.6-flash-high",
			paramID:    "effort",
			paramValue: "high",
			alias:      "gemini-flash",
		},
		{
			file:       "gpt-5.4.json",
			publicID:   "gpt-5.4",
			wireID:     "gpt-5.4-medium",
			paramID:    "reasoning",
			paramValue: "medium",
		},
		{
			file:       "composer-2.5.json",
			publicID:   "composer-2.5",
			wireID:     "composer-2.5-fast",
			paramID:    "fast",
			paramValue: "true",
			alias:      "composer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.publicID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "catalog", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			m, ok := mapAvailableModel(json.RawMessage(raw))
			if !ok {
				t.Fatal("mapAvailableModel failed")
			}
			if m.ID != tc.publicID {
				t.Fatalf("id = %q", m.ID)
			}
			sel := SelectionFromModel(m)
			if sel.PublicID != tc.publicID {
				t.Fatalf("public = %q", sel.PublicID)
			}
			if sel.WireModelID != tc.wireID {
				t.Fatalf("wire = %q want %q", sel.WireModelID, tc.wireID)
			}
			if sel.MaxMode != tc.maxMode {
				t.Fatalf("max_mode = %v", sel.MaxMode)
			}
			found := false
			for _, p := range sel.Parameters {
				if p.ID == tc.paramID && p.Value == tc.paramValue {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing param %s=%s in %#v", tc.paramID, tc.paramValue, sel.Parameters)
			}
			if tc.alias != "" && !modelMatchesID(m, tc.alias) {
				t.Fatalf("alias %q did not match", tc.alias)
			}
			if tc.legacyMatch != "" && !modelMatchesID(m, tc.legacyMatch) {
				t.Fatalf("legacy %q did not match", tc.legacyMatch)
			}

			payload, err := BuildRunPayloadSelection(sel, ParsedChat{
				SystemPrompt: "sys",
				UserText:     "hi",
			})
			if err != nil {
				t.Fatal(err)
			}
			if payload.ModelID != tc.publicID {
				t.Fatalf("response model = %q", payload.ModelID)
			}
		})
	}
}
