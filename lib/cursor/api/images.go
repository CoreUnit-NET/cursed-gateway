package cursor_api_sdk

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// Image is a decoded OpenAI/OpenCode image attachment (data-URL or raw base64 only).
type Image struct {
	Bytes    []byte
	MimeType string
	Filename string
}

var dataURLRe = regexp.MustCompile(`(?i)^data:([^;,]+)?(?:;charset=[^;,]+)?;base64,([A-Za-z0-9+/=\s]+)$`)

// extractImagesFromContent mirrors otto openai/images.ts: image_url / image /
// input_image / file parts with data:…;base64,… or raw base64. http(s) URLs are ignored.
func extractImagesFromContent(raw json.RawMessage) []Image {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		// Single part object.
		if img, ok := imageFromPart(raw); ok {
			return []Image{img}
		}
		return nil
	}
	out := make([]Image, 0, len(parts))
	for _, part := range parts {
		if img, ok := imageFromPart(part); ok {
			out = append(out, img)
		}
	}
	return out
}

func imageFromPart(raw json.RawMessage) (Image, bool) {
	var part struct {
		Type     string          `json:"type"`
		Filename string          `json:"filename"`
		Name     string          `json:"name"`
		Data     string          `json:"data"`
		MimeType string          `json:"mime_type"`
		Mime     string          `json:"mime"`
		URL      string          `json:"url"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &part); err != nil {
		return Image{}, false
	}
	typ := strings.ToLower(strings.TrimSpace(part.Type))
	filename := strings.TrimSpace(part.Filename)
	if filename == "" {
		filename = strings.TrimSpace(part.Name)
	}
	if filename == "" {
		filename = "attachment"
	}
	mimeHint := strings.TrimSpace(part.MimeType)
	if mimeHint == "" {
		mimeHint = strings.TrimSpace(part.Mime)
	}

	switch typ {
	case "image_url", "image", "input_image":
		if url := imageURLFromPart(part.ImageURL, part.URL); strings.HasPrefix(url, "data:") {
			if img, ok := decodeDataURL(url); ok {
				if strings.Contains(filename, ".") {
					img.Filename = filename
				}
				return img, true
			}
		}
		if data := strings.TrimSpace(part.Data); data != "" {
			if bytes, ok := decodeBase64Payload(data); ok {
				mime := mimeHint
				if mime == "" {
					mime = guessMimeFromName(filename)
				}
				if mime == "" {
					mime = "image/png"
				}
				return Image{Bytes: bytes, MimeType: mime, Filename: filename}, true
			}
		}
		return Image{}, false

	case "file", "input_file":
		mime := strings.ToLower(mimeHint)
		looksImage := strings.HasPrefix(mime, "image/") || imageExtName(filename)
		if !looksImage {
			return Image{}, false
		}
		if data := strings.TrimSpace(part.Data); data != "" {
			if bytes, ok := decodeBase64Payload(data); ok {
				if mime == "" {
					mime = guessMimeFromName(filename)
				}
				if mime == "" {
					mime = "image/png"
				}
				return Image{Bytes: bytes, MimeType: mime, Filename: filename}, true
			}
		}
		if url := imageURLFromPart(part.ImageURL, part.URL); strings.HasPrefix(url, "data:") {
			if img, ok := decodeDataURL(url); ok {
				if strings.Contains(filename, ".") {
					img.Filename = filename
				}
				if mime != "" {
					img.MimeType = mime
				}
				return img, true
			}
		}
		return Image{}, false

	default:
		return Image{}, false
	}
}

func imageURLFromPart(imageURL json.RawMessage, fallback string) string {
	if len(imageURL) > 0 && string(imageURL) != "null" {
		var asString string
		if err := json.Unmarshal(imageURL, &asString); err == nil {
			if s := strings.TrimSpace(asString); s != "" {
				return s
			}
		}
		var obj struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(imageURL, &obj); err == nil {
			if s := strings.TrimSpace(obj.URL); s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func decodeDataURL(dataURL string) (Image, bool) {
	m := dataURLRe.FindStringSubmatch(strings.TrimSpace(dataURL))
	if m == nil {
		return Image{}, false
	}
	mime := strings.TrimSpace(m[1])
	if mime == "" {
		mime = "application/octet-stream"
	}
	bytes, ok := decodeBase64Payload(m[2])
	if !ok {
		return Image{}, false
	}
	ext := "bin"
	lower := strings.ToLower(mime)
	switch {
	case strings.Contains(lower, "png"):
		ext = "png"
	case strings.Contains(lower, "jpeg"), strings.Contains(lower, "jpg"):
		ext = "jpg"
	case strings.Contains(lower, "gif"):
		ext = "gif"
	case strings.Contains(lower, "webp"):
		ext = "webp"
	}
	return Image{
		Bytes:    bytes,
		MimeType: mime,
		Filename: "attachment." + ext,
	}, true
}

func decodeBase64Payload(data string) ([]byte, bool) {
	cleaned := strings.TrimSpace(data)
	if i := strings.Index(cleaned, ","); strings.HasPrefix(strings.ToLower(cleaned), "data:") && i >= 0 {
		cleaned = cleaned[i+1:]
	}
	cleaned = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, cleaned)
	out, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func guessMimeFromName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".bmp"):
		return "image/bmp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	default:
		return ""
	}
}

func imageExtName(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
