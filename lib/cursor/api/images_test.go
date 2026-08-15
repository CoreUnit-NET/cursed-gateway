package cursor_api_sdk

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestExtractImagesFromContentDataURL(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"what color?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,` + tinyPNGBase64 + `"}}]`)
	imgs := extractImagesFromContent(raw)
	if len(imgs) != 1 {
		t.Fatalf("images = %d, want 1", len(imgs))
	}
	want, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(imgs[0].Bytes) != string(want) {
		t.Fatalf("bytes mismatch: got %d want %d", len(imgs[0].Bytes), len(want))
	}
	if imgs[0].MimeType != "image/png" {
		t.Fatalf("mime = %q", imgs[0].MimeType)
	}
	if imgs[0].Filename != "attachment.png" {
		t.Fatalf("filename = %q", imgs[0].Filename)
	}
}

func TestExtractImagesIgnoresHTTPS(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`)
	if imgs := extractImagesFromContent(raw); len(imgs) != 0 {
		t.Fatalf("expected https ignored, got %#v", imgs)
	}
}

func TestExtractImagesRawBase64FilePart(t *testing.T) {
	raw := json.RawMessage(`[{"type":"file","filename":"dot.png","mime_type":"image/png","data":"` + tinyPNGBase64 + `"}]`)
	imgs := extractImagesFromContent(raw)
	if len(imgs) != 1 {
		t.Fatalf("images = %d, want 1", len(imgs))
	}
	if imgs[0].Filename != "dot.png" || imgs[0].MimeType != "image/png" {
		t.Fatalf("img = %#v", imgs[0])
	}
}

func TestChatMessageUnmarshalExtractsImages(t *testing.T) {
	raw := `{"role":"user","content":[{"type":"text","text":"caption"},{"type":"image_url","image_url":{"url":"data:image/png;base64,` + tinyPNGBase64 + `"}}]}`
	var msg ChatMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Content != "caption" {
		t.Fatalf("content = %q", msg.Content)
	}
	if len(msg.Images) != 1 {
		t.Fatalf("images = %d", len(msg.Images))
	}
}
