package cursor_api_sdk

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func parseConnectEndStream(payload []byte) error {
	var doc connectEndError
	if err := json.Unmarshal(payload, &doc); err != nil {
		return fmt.Errorf("connect end-stream parse: %w", err)
	}
	if doc.Error.Code == "" && doc.Error.Message == "" {
		return nil
	}
	return classifyConnectCode(doc.Error.Code, doc.Error.Message)
}
