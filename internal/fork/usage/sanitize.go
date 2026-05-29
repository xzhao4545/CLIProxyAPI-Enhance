package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

func sanitizeProviderError(raw string, maxBytes int) (message string, stored string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	raw = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, raw)
	raw = strings.Join(strings.Fields(raw), " ")
	if maxBytes <= 0 {
		maxBytes = defaultMaxProviderErrorBytes
	}
	stored = truncateUTF8(raw, maxBytes)
	message = truncateUTF8(stored, 512)
	return message, stored
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	out := value[:maxBytes]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}

func hashClientKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func successRate(success, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(success) / float64(total)
}
