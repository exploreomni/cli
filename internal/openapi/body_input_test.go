package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// --body transport
//
// The API answers any non-JSON body with a generic 400 "Invalid JSON", which
// reads like a body-SHAPE problem. These tests pin the client-side handling
// that keeps callers out of that dead end: @file input, and rejecting a body
// that isn't JSON before any network call.
// ---------------------------------------------------------------------------

// writeTempBody writes content to a temp file and returns its path.
func writeTempBody(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp body: %v", err)
	}
	return path
}

// "--body @file.json" reads the body from disk, byte for byte.
func TestResolveBody_AtFile(t *testing.T) {
	content := `{"modelId":"abc","prompt":"hi"}`
	path := writeTempBody(t, "body.json", content)

	got, err := resolveBody("@"+path, "body", true)
	if err != nil {
		t.Fatalf("resolveBody: %v", err)
	}
	if string(got) != content {
		t.Errorf("body = %q, want %q", string(got), content)
	}
}

// A missing @file is a client-side error that names the path.
func TestResolveBody_AtFileMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")

	_, err := resolveBody("@"+missing, "body", true)
	if err == nil {
		t.Fatal("expected an error for a missing body file, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want it to name the path %q", err.Error(), missing)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to say the file was not found", err.Error())
	}
}

// The 10 MB stdin cap applies to @file input too.
func TestResolveBody_AtFileTooLarge(t *testing.T) {
	path := writeTempBody(t, "big.json", strings.Repeat("x", maxStdinSize+1))

	_, err := resolveBody("@"+path, "body", true)
	if err == nil {
		t.Fatal("expected an error for an oversized body file, got nil")
	}
	if !strings.Contains(err.Error(), "10 MB") {
		t.Errorf("error = %q, want it to mention 10 MB", err.Error())
	}
}

// "@" with no path is a usage error, not a stat of the empty string.
func TestResolveBody_AtWithoutPath(t *testing.T) {
	if _, err := resolveBody("@", "body", true); err == nil {
		t.Fatal("expected an error for a bare @, got nil")
	}
}

// A file whose contents aren't JSON is rejected before the request is made.
func TestResolveBody_AtFileInvalidJSON(t *testing.T) {
	path := writeTempBody(t, "body.json", "modelId: abc\n")

	_, err := resolveBody("@"+path, "body", true)
	if err == nil {
		t.Fatal("expected an error for a non-JSON body file, got nil")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %q, want it to say the body is not valid JSON", err.Error())
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the file %q", err.Error(), path)
	}
}

// The curl-style mistake: passing a bare file path as the body. The error must
// point at the @file / stdin forms rather than leave the caller re-reading the
// body schema.
func TestResolveBody_BareFilePathHint(t *testing.T) {
	path := writeTempBody(t, "body.json", `{"a":1}`)

	for _, raw := range []string{path, "/tmp/does-not-exist.json", "./body.json", "~/body.json"} {
		_, err := resolveBody(raw, "body", true)
		if err == nil {
			t.Fatalf("resolveBody(%q): expected an error, got nil", raw)
		}
		msg := err.Error()
		if !strings.Contains(msg, "looks like a file path") {
			t.Errorf("resolveBody(%q) error = %q, want the file-path hint", raw, msg)
		}
		if !strings.Contains(msg, "--body @"+raw) {
			t.Errorf("resolveBody(%q) error = %q, want it to suggest --body @%s", raw, msg, raw)
		}
		if !strings.Contains(msg, "--body - < "+raw) {
			t.Errorf("resolveBody(%q) error = %q, want it to suggest the stdin form", raw, msg)
		}
	}
}

// Non-JSON that isn't path-shaped gets a plain parse error with a position.
func TestResolveBody_InvalidJSONDiagnostic(t *testing.T) {
	_, err := resolveBody(`{"a":1,}`, "body", true)
	if err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not valid JSON") {
		t.Errorf("error = %q, want it to say the body is not valid JSON", msg)
	}
	if !strings.Contains(msg, "byte ") {
		t.Errorf("error = %q, want it to include the parse position", msg)
	}
	if strings.Contains(msg, "looks like a file path") {
		t.Errorf("error = %q, should not offer the file-path hint here", msg)
	}
}

// The error names whichever flag the caller actually used.
func TestResolveBody_NamesTheFlagUsed(t *testing.T) {
	_, err := resolveBody("not json", "json-body", true)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "--json-body") {
		t.Errorf("error = %q, want it to name --json-body", err.Error())
	}
}

// Valid JSON passes through byte-identical: no reformatting, no re-ordering.
func TestResolveBody_ValidJSONPassthrough(t *testing.T) {
	cases := []string{
		`{"b":1,   "a":  [1,2,3]}`,
		`  {"padded": true}  `,
		`[1,2,3]`,
		`"a bare string"`,
		`null`,
	}
	for _, raw := range cases {
		got, err := resolveBody(raw, "body", true)
		if err != nil {
			t.Fatalf("resolveBody(%q): %v", raw, err)
		}
		if string(got) != raw {
			t.Errorf("resolveBody(%q) = %q, want the bytes unchanged", raw, string(got))
		}
	}
}

// A quoted path with spaces reaches us looking ordinary — it still gets the
// hint, and the suggested commands come back re-quoted so they can be pasted.
func TestResolveBody_PathWithSpacesHint(t *testing.T) {
	path := writeTempBody(t, "request body.json", `{"a":1}`)

	_, err := resolveBody(path, "body", true)
	if err == nil {
		t.Fatalf("resolveBody(%q): expected an error, got nil", path)
	}
	msg := err.Error()
	if !strings.Contains(msg, "looks like a file path") {
		t.Errorf("error = %q, want the file-path hint", msg)
	}
	if !strings.Contains(msg, `--body "@`+path+`"`) {
		t.Errorf("error = %q, want a quoted --body @%s suggestion", msg, path)
	}
	if !strings.Contains(msg, `--body - < "`+path+`"`) {
		t.Errorf("error = %q, want a quoted stdin suggestion", msg)
	}
}

// Non-JSON media types (the multipart upload endpoints) skip validation.
func TestResolveBody_SkipsValidationForNonJSON(t *testing.T) {
	raw := "--boundary\r\nnot json\r\n"
	got, err := resolveBody(raw, "body", false)
	if err != nil {
		t.Fatalf("resolveBody: %v", err)
	}
	if string(got) != raw {
		t.Errorf("body = %q, want it unchanged", string(got))
	}
}

// Skipping JSON validation must not skip the transport diagnostics: a
// multipart operation handed a bare path still gets the @file hint.
func TestResolveBody_NonJSONStillGetsPathHint(t *testing.T) {
	_, err := resolveBody("/tmp/request.multipart", "body", false)
	if err == nil {
		t.Fatal("expected the file-path hint for a non-JSON body, got nil")
	}
	if !strings.Contains(err.Error(), "--body @/tmp/request.multipart") {
		t.Errorf("error = %q, want it to suggest --body @/tmp/request.multipart", err.Error())
	}

	// An existing file named as a bare path counts too, whatever its contents.
	path := writeTempBody(t, "upload.bin", "\x00\x01binary")
	if _, err := resolveBody(path, "body", false); err == nil {
		t.Fatalf("resolveBody(%q): expected the file-path hint, got nil", path)
	}
}

// The empty-body error is a transport check, so it applies to every media type.
func TestResolveBody_EmptyNonJSON(t *testing.T) {
	if _, err := resolveBody("", "body", false); err == nil {
		t.Fatal("expected an error for an explicitly empty --body, got nil")
	}
}

// "-" still reads stdin, and stdin is validated the same way.
func TestResolveBody_Stdin(t *testing.T) {
	withStdin(t, `{"from":"stdin"}`, func() {
		got, err := resolveBody("-", "body", true)
		if err != nil {
			t.Fatalf("resolveBody: %v", err)
		}
		if string(got) != `{"from":"stdin"}` {
			t.Errorf("body = %q, want the stdin bytes", string(got))
		}
	})

	withStdin(t, "not json", func() {
		_, err := resolveBody("-", "body", true)
		if err == nil {
			t.Fatal("expected an error for non-JSON stdin, got nil")
		}
		if !strings.Contains(err.Error(), "stdin") {
			t.Errorf("error = %q, want it to name stdin as the source", err.Error())
		}
	})
}

// "@-" is the curl spelling of stdin.
func TestResolveBody_AtDashIsStdin(t *testing.T) {
	withStdin(t, `{"from":"stdin"}`, func() {
		got, err := resolveBody("@-", "body", true)
		if err != nil {
			t.Fatalf("resolveBody: %v", err)
		}
		if string(got) != `{"from":"stdin"}` {
			t.Errorf("body = %q, want the stdin bytes", string(got))
		}
	})
}

// An empty source is reported as empty rather than as a JSON syntax error.
func TestResolveBody_EmptySources(t *testing.T) {
	path := writeTempBody(t, "empty.json", "")
	if _, err := resolveBody("@"+path, "body", true); err == nil {
		t.Fatal("expected an error for an empty body file, got nil")
	} else if !strings.Contains(err.Error(), "no request body") {
		t.Errorf("error = %q, want it to report an empty body", err.Error())
	}

	if _, err := resolveBody("   ", "body", true); err == nil {
		t.Fatal("expected an error for a whitespace-only --body, got nil")
	}
}

// withStdin swaps os.Stdin for a pipe carrying content for the duration of fn.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig; r.Close() }()

	go func() {
		w.Write([]byte(content))
		w.Close()
	}()
	fn()
}

// ---------------------------------------------------------------------------
// End-to-end through a generated command
// ---------------------------------------------------------------------------

func bodyCmd(t *testing.T, exec Executor) *cobra.Command {
	t.Helper()
	cmd := buildCommand(&operationInfo{
		Tag:         "test",
		OperationID: "testCreateWidget",
		Method:      "POST",
		Path:        "/api/v1/widgets",
		HasBody:     true,
	}, exec)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

// "omni ... --body @file.json" sends the file's bytes.
func TestBuildCommand_BodyAtFile(t *testing.T) {
	content := `{"key":"val"}`
	path := writeTempBody(t, "body.json", content)

	var captured APIRequest
	cmd := bodyCmd(t, func(req APIRequest) error { captured = req; return nil })
	cmd.SetArgs([]string{"--body", "@" + path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(captured.Body) != content {
		t.Errorf("body = %q, want %q", string(captured.Body), content)
	}
}

// The hidden --json-body alias gets identical treatment.
func TestBuildCommand_JSONBodyAtFile(t *testing.T) {
	content := `{"key":"val"}`
	path := writeTempBody(t, "body.json", content)

	var captured APIRequest
	cmd := bodyCmd(t, func(req APIRequest) error { captured = req; return nil })
	cmd.SetArgs([]string{"--json-body", "@" + path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(captured.Body) != content {
		t.Errorf("body = %q, want %q", string(captured.Body), content)
	}
}

// An invalid body must fail before the executor runs — no request is made.
func TestBuildCommand_InvalidBodyMakesNoRequest(t *testing.T) {
	called := false
	cmd := bodyCmd(t, func(req APIRequest) error { called = true; return nil })
	cmd.SetArgs([]string{"--body", "/tmp/some-body.json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a bare file path, got nil")
	}
	if called {
		t.Error("executor ran despite an invalid body")
	}
	if !strings.Contains(err.Error(), "looks like a file path") {
		t.Errorf("error = %q, want the file-path hint", err.Error())
	}
}

// A multipart operation keeps the transport diagnostics but not the JSON
// validity check.
func TestBuildCommand_NonJSONBodyOperation(t *testing.T) {
	op := &operationInfo{
		Tag:         "uploads",
		OperationID: "uploadsCreate",
		Method:      "POST",
		Path:        "/api/v1/uploads",
		HasBody:     true,
		BodyNonJSON: true,
	}

	// Non-JSON content goes through untouched.
	var captured APIRequest
	cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"--body", "not json at all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(captured.Body) != "not json at all" {
		t.Errorf("body = %q, want it unchanged", string(captured.Body))
	}

	// A bare path still gets caught before the request.
	called := false
	cmd2 := buildCommand(op, func(req APIRequest) error { called = true; return nil })
	cmd2.SilenceUsage, cmd2.SilenceErrors = true, true
	cmd2.SetArgs([]string{"--body", "/tmp/upload.csv"})
	err := cmd2.Execute()
	if err == nil {
		t.Fatal("expected the file-path hint, got nil")
	}
	if called {
		t.Error("executor ran with a path as the body")
	}
	if !strings.Contains(err.Error(), "--body @/tmp/upload.csv") {
		t.Errorf("error = %q, want it to suggest --body @/tmp/upload.csv", err.Error())
	}
}

// An explicitly empty --body is a typo (usually a shell variable that didn't
// expand), not a request to send nothing. Omitting the flag still sends no body.
func TestBuildCommand_ExplicitlyEmptyBody(t *testing.T) {
	for _, flag := range []string{"--body", "--json-body"} {
		called := false
		cmd := bodyCmd(t, func(req APIRequest) error { called = true; return nil })
		cmd.SetArgs([]string{flag, ""})

		err := cmd.Execute()
		if err == nil {
			t.Fatalf("%s '': expected an error, got nil", flag)
		}
		if called {
			t.Errorf("%s '': executor ran despite an empty body", flag)
		}
		if !strings.Contains(err.Error(), flag+" is empty") {
			t.Errorf("%s '' error = %q, want it to report the empty flag", flag, err.Error())
		}
	}

	// Omitting the flag entirely is still a bodiless request.
	var captured APIRequest
	sent := false
	cmd := bodyCmd(t, func(req APIRequest) error { captured = req; sent = true; return nil })
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute without --body: %v", err)
	}
	if !sent {
		t.Fatal("executor did not run without --body")
	}
	if captured.Body != nil {
		t.Errorf("body = %q, want nil when --body is omitted", string(captured.Body))
	}
}

// The same holds on a shorthand command: an empty --body isn't shorthand input.
func TestBodyShorthand_ExplicitlyEmptyBody(t *testing.T) {
	called := false
	op := &operationInfo{
		Tag:         "ai",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}
	cmd := buildCommand(op, func(req APIRequest) error { called = true; return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--body", "", "some question"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an empty --body, got nil")
	}
	if called {
		t.Error("executor ran despite an empty body")
	}
	if !strings.Contains(err.Error(), "--body is empty") {
		t.Errorf("error = %q, want it to report the empty flag", err.Error())
	}
}

// Body shorthand assembles its own JSON and must not trip the new validation.
func TestBodyShorthand_SurvivesBodyValidation(t *testing.T) {
	var captured APIRequest
	op := &operationInfo{
		Tag:         "ai",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}
	cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"How do I add a format to a dimension?"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(captured.Body) != `{"question":"How do I add a format to a dimension?"}` {
		t.Errorf("body = %q, want the assembled shorthand JSON", string(captured.Body))
	}
}

// Shorthand commands accept --body @file for the full JSON form.
func TestBodyShorthand_AtFileBody(t *testing.T) {
	content := `{"question":"why?"}`
	path := writeTempBody(t, "body.json", content)

	var captured APIRequest
	op := &operationInfo{
		Tag:         "ai",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}
	cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--body", "@" + path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(captured.Body) != content {
		t.Errorf("body = %q, want %q", string(captured.Body), content)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandHome("~/x.json"); got != filepath.Join(home, "x.json") {
		t.Errorf("expandHome(~/x.json) = %q, want %q", got, filepath.Join(home, "x.json"))
	}
	// "~" only expands as a path prefix, never mid-string.
	if got := expandHome("/tmp/~/x.json"); got != "/tmp/~/x.json" {
		t.Errorf("expandHome = %q, want it unchanged", got)
	}
}
