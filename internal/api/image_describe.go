package api

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// describeImage turns image metadata into a compact text marker that can be
// inlined into the prompt when the underlying transport (Windsurf gRPC) does
// not yet support binary image payloads.
//
// Typical outputs:
//
//	[image: image/png, 124 KB]
//	[image: https://example.com/a.png]
//	[image: data URL, image/jpeg, 88 KB]
//
// The goal is informational: the model should at least know that the user
// attached an image, even if the raw bytes never reach the upstream.
func describeImage(mediaType, data, sourceURL string) string {
	mediaType = strings.TrimSpace(mediaType)

	switch {
	case sourceURL != "" && !strings.HasPrefix(strings.ToLower(sourceURL), "data:"):
		// Plain remote URL.
		label := sourceURL
		if mediaType != "" {
			return fmt.Sprintf("[image: %s, %s]", mediaType, label)
		}
		return fmt.Sprintf("[image: %s]", label)

	case sourceURL != "" && strings.HasPrefix(strings.ToLower(sourceURL), "data:"):
		// data:<media>;base64,<data>
		mt, payload := parseDataURL(sourceURL)
		if mt != "" && mediaType == "" {
			mediaType = mt
		}
		size := approxDecodedSize(payload)
		return fmt.Sprintf("[image: data URL, %s, %s]", orDefault(mediaType, "unknown"), formatSize(size))

	case data != "":
		// Raw base64 payload (Anthropic source.type == base64).
		size := approxDecodedSize(data)
		return fmt.Sprintf("[image: %s, %s]", orDefault(mediaType, "unknown"), formatSize(size))

	default:
		if mediaType != "" {
			return fmt.Sprintf("[image: %s]", mediaType)
		}
		return "[image]"
	}
}

// parseDataURL splits a data: URL into (mediaType, base64Payload). Returns
// empty strings if the input is malformed.
func parseDataURL(s string) (mediaType, payload string) {
	// data:[<media-type>][;base64],<data>
	if !strings.HasPrefix(strings.ToLower(s), "data:") {
		return "", ""
	}
	body := s[5:]
	comma := strings.Index(body, ",")
	if comma < 0 {
		return "", ""
	}
	header := body[:comma]
	payload = body[comma+1:]

	// Strip ";base64" if present; trim parameters like charset.
	parts := strings.Split(header, ";")
	if len(parts) > 0 {
		mediaType = parts[0]
	}
	// URL-decode non-base64 payloads so size reflects actual bytes.
	if !containsIgnoreCase(parts, "base64") {
		if decoded, err := url.QueryUnescape(payload); err == nil {
			payload = decoded
		}
	}
	return mediaType, payload
}

// approxDecodedSize returns the decoded byte size of a base64 string, with a
// best-effort fallback to the raw string length when decoding fails.
func approxDecodedSize(b64 string) int {
	trimmed := strings.TrimSpace(b64)
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return len(decoded)
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
		return len(decoded)
	}
	// Rough estimate: base64 inflates by ~4/3.
	return (len(trimmed) * 3) / 4
}

// formatSize renders a byte count in human-friendly units (KB/MB).
func formatSize(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%d KB", n/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func containsIgnoreCase(xs []string, target string) bool {
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), target) {
			return true
		}
	}
	return false
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
