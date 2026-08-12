package cursor_api_sdk

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

const (
	flagRaw       = 0x00
	flagGzip      = 0x01
	flagEndStream = 0x02
)

// FrameConnect wraps a protobuf payload in a Connect data frame.
func FrameConnect(payload []byte, flags byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

type connectFrame struct {
	Flags   byte
	Payload []byte
}

func readConnectFrame(r io.Reader) (connectFrame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return connectFrame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[1:5])
	if n > 32<<20 {
		return connectFrame{}, fmt.Errorf("connect frame too large: %d", n)
	}
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return connectFrame{}, err
		}
	}
	return connectFrame{Flags: hdr[0], Payload: payload}, nil
}

type connectEndError struct {
	Error struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details []connectDetail `json:"details"`
	} `json:"error"`
}

type connectDetail struct {
	Type  string          `json:"type"`
	Debug json.RawMessage `json:"debug"`
}

type connectDebugPayload struct {
	Error   string `json:"error"`
	Details *struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"details"`
}

func parseConnectEndStream(payload []byte) error {
	var doc connectEndError
	if err := json.Unmarshal(payload, &doc); err != nil {
		slog.Error("connect end-stream parse failed", "raw", truncateForLog(payload, 512), "err", err)
		return fmt.Errorf("connect end-stream parse: %w", err)
	}
	if doc.Error.Code == "" && doc.Error.Message == "" {
		return nil
	}
	dbg := extractConnectDebug(doc.Error.Details)
	// Upstream-reported stream failure; Warn matches other API-layer upstream errors.
	slog.Warn("connect end-stream error",
		"code", doc.Error.Code,
		"message", doc.Error.Message,
		"debug_error", dbg.Error,
		"title", dbg.Title,
		"detail", dbg.Detail,
		"raw", truncateForLog(payload, 512),
	)
	return classifyConnectCode(doc.Error.Code, doc.Error.Message, dbg)
}

func extractConnectDebug(details []connectDetail) connectDebugInfo {
	var out connectDebugInfo
	for _, d := range details {
		if len(d.Debug) == 0 {
			continue
		}
		var dbg connectDebugPayload
		if err := json.Unmarshal(d.Debug, &dbg); err != nil {
			continue
		}
		if dbg.Error != "" {
			out.Error = dbg.Error
		}
		if dbg.Details != nil {
			if dbg.Details.Title != "" {
				out.Title = dbg.Details.Title
			}
			if dbg.Details.Detail != "" {
				out.Detail = dbg.Details.Detail
			}
		}
		if out.Error != "" || out.Detail != "" {
			return out
		}
	}
	return out
}

func truncateForLog(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
