package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// resolveBody turns a raw --body/--json-body flag value into the bytes to send.
//
//	"-"        read from stdin
//	"@path"    read from a file ("@-" is stdin too, curl-style)
//	anything else is the literal body
//
// When validateJSON is set the result is checked with json.Valid before any
// network call, because the API answers a non-JSON body with a generic
// "Invalid JSON" 400 that reads like a body-shape problem. flagName is the flag
// the value came from, so the error text names the flag the caller typed.
//
// The bytes are never re-serialized — what the caller supplied is what gets
// sent.
func resolveBody(raw, flagName string, validateJSON bool) ([]byte, error) {
	switch {
	case raw == "-":
		data, err := readStdin()
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return checkBody(data, raw, flagName, "stdin", validateJSON)

	case strings.HasPrefix(raw, "@"):
		path := strings.TrimPrefix(raw, "@")
		if path == "-" {
			data, err := readStdin()
			if err != nil {
				return nil, fmt.Errorf("reading stdin: %w", err)
			}
			return checkBody(data, raw, flagName, "stdin", validateJSON)
		}
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("--%s: no file path after \"@\"; use --%s @path/to/body.json", flagName, flagName)
		}
		path = expandHome(path)
		data, err := readBodyFile(path)
		if err != nil {
			return nil, err
		}
		return checkBody(data, raw, flagName, fmt.Sprintf("file %s", path), validateJSON)

	default:
		return checkBody([]byte(raw), raw, flagName, "", validateJSON)
	}
}

// readBodyFile reads a body file, enforcing the same size cap as stdin.
func readBodyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("body file not found: %s", path)
		}
		return nil, fmt.Errorf("reading body file %s: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxStdinSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading body file %s: %w", path, err)
	}
	if len(data) > maxStdinSize {
		return nil, fmt.Errorf("body file %s exceeds maximum size of 10 MB", path)
	}
	return data, nil
}

// checkBody validates data and returns it unchanged. source describes where
// the bytes came from ("stdin", "file X") or is empty when they came straight
// from the flag value — only in that case can the raw value itself be a
// mistyped file path.
//
// Transport checks (empty input, a path-shaped flag value) run for every
// operation; only the JSON validity check is conditional, so the multipart
// upload endpoints still get the "--body @path" hint they'd otherwise need
// most.
func checkBody(data []byte, raw, flagName, source string, validateJSON bool) ([]byte, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		if source != "" {
			return nil, fmt.Errorf("no request body read from %s", source)
		}
		return nil, fmt.Errorf("--%s is empty; omit the flag to send no body", flagName)
	}

	// Valid JSON is never a path, so this test comes first: it stops a file
	// that happens to be named "{}" from hijacking a legitimate body.
	if validateJSON && json.Valid(data) {
		return data, nil
	}

	if source == "" && looksLikePath(raw) {
		return nil, fmt.Errorf("--%s looks like a file path, not a request body: %s\nread the file instead: --%s %s   (or --%s - < %s)",
			flagName, raw, flagName, shellQuote("@"+raw), flagName, shellQuote(raw))
	}

	if !validateJSON {
		return data, nil
	}

	where := "--" + flagName
	if source != "" {
		where = source
	}
	return nil, fmt.Errorf("request body from %s is not valid JSON: %s", where, jsonProblem(data))
}

// shellQuote wraps a value in double quotes when it contains whitespace, so the
// suggested command in an error message can be pasted as-is.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// looksLikePath reports whether a flag value is more plausibly a file path than
// a request body — an absolute/relative path prefix, or the name of a file that
// actually exists. Paths with spaces count: the shell strips the quotes, so the
// value arrives here looking ordinary.
func looksLikePath(raw string) bool {
	if raw == "" {
		return false
	}
	for _, prefix := range []string{"/", "./", "../", "~/"} {
		if strings.HasPrefix(raw, prefix) {
			return true
		}
	}
	// A multi-line value is a pasted document, not a path worth stat-ing.
	if strings.ContainsAny(raw, "\r\n") {
		return false
	}
	if info, err := os.Stat(expandHome(raw)); err == nil && !info.IsDir() {
		return true
	}
	return false
}

// jsonProblem renders a short parse diagnostic: the syntax error plus the few
// bytes around the offset it points at.
func jsonProblem(data []byte) string {
	var v json.RawMessage
	err := json.Unmarshal(data, &v)
	if err == nil {
		return "unexpected trailing data"
	}

	syn, ok := err.(*json.SyntaxError)
	if !ok {
		return err.Error()
	}

	offset := int(syn.Offset)
	if offset < 0 || offset > len(data) {
		return syn.Error()
	}
	start := offset - 20
	if start < 0 {
		start = 0
	}
	end := offset + 20
	if end > len(data) {
		end = len(data)
	}
	return fmt.Sprintf("%s (at byte %d, near %q)", syn.Error(), offset, string(data[start:end]))
}

// expandHome expands a leading "~/" — shells don't expand it after "@".
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}
