package output

import (
	"strings"
	"unicode"
)

// Text from the API and the warehouse is data, not terminal instructions: an
// ESC in a value would otherwise let it retitle the window or write the
// clipboard (OSC 52) when rendered. JSON and passed-through payloads are
// written as they arrived; only human rendering goes through these.

// sanitize drops control characters, keeping newlines and tabs.
func sanitize(s string) string {
	if strings.IndexFunc(s, isUnsafe) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// singleLine sanitizes text bound for one cell of a layout, where a newline
// would break the rows.
func singleLine(s string) string {
	s = sanitize(s)
	if strings.ContainsAny(s, "\n\t") {
		s = strings.NewReplacer("\n", " ", "\t", " ").Replace(s)
	}
	return s
}

func isUnsafe(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t'
}

// sanitizeJSON cleans every string and key in a decoded JSON value.
func sanitizeJSON(v any) any {
	switch x := v.(type) {
	case string:
		return sanitize(x)
	case []any:
		for i := range x {
			x[i] = sanitizeJSON(x[i])
		}
	case map[string]any:
		for k, val := range x {
			if clean := sanitize(k); clean != k {
				delete(x, k)
				k = clean
			}
			x[k] = sanitizeJSON(val)
		}
	}
	return v
}
