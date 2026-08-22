package completion_api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func parseBoolish(raw json.RawMessage, dest *bool) error {
	if len(raw) == 0 || string(raw) == "null" {
		*dest = false
		return nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err == nil {
		*dest = v
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1", "yes":
			*dest = true
		default:
			*dest = false
		}
		return nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		*dest = n != 0
		return nil
	}
	return fmt.Errorf("stream must be a boolean")
}

func validateN(n *int) error {
	if n != nil && *n != 1 {
		return fmt.Errorf("n must be 1")
	}
	return nil
}

func includeStreamUsage(opts *StreamOptions) bool {
	return opts != nil && opts.IncludeUsage
}

func newChatCompletionID() string {
	return "chatcmpl-" + randomHex(12)
}

func newTextCompletionID() string {
	return "cmpl-" + randomHex(12)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
