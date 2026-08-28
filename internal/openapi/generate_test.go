package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Helper unit tests
//
// These test the small string-manipulation functions that turn OpenAPI
// identifiers into CLI-friendly names. For example, the API operation
// "ModelsList" needs to become the CLI command "models list".
// ---------------------------------------------------------------------------

// slugify lowercases a string and replaces spaces/underscores with hyphens.
// It's used to turn API tag names like "User Attributes" into command group
// names like "user-attributes".
func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"User Attributes", "user-attributes"},
		{"SCIM", "scim"},
		{"already-lower", "already-lower"},
		{"under_score", "under-score"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Command names come from the same slug renderer as flag names. Unifying them
// was only safe because it changes no command name generated from the real spec
// (TestRealSpec_CommandNamesAreStable) — command spellings have no
// normalization fallback, so any change would break users.
func TestCanonicalName_OperationIDs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ModelsList", "models-list"},
		{"aiJobStatus", "ai-job-status"},
		{"a", "a"},
		{"ABC", "abc"},
	}
	for _, c := range cases {
		if got := canonicalName(c.in); got != c.want {
			t.Errorf("canonicalName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// boolVal safely dereferences a *bool pointer — the OpenAPI spec uses
// pointer bools for fields like "required" and "deprecated" that may be absent.
func TestBoolVal(t *testing.T) {
	tr, fa := true, false
	cases := []struct {
		in   *bool
		want bool
	}{
		{nil, false},
		{&tr, true},
		{&fa, false},
	}
	for _, c := range cases {
		if got := boolVal(c.in); got != c.want {
			t.Errorf("boolVal(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// commandName derives the CLI subcommand name from an API operation. It strips
// the tag prefix from the operationId so that e.g. "ModelsList" under the
// "Models" tag becomes just "list" (the user types "omni models list").
// When there's no operationId, it falls back to "method-resource".
func TestCommandName(t *testing.T) {
	// With operationID that has tag prefix → strip it.
	// "ModelsList" under tag "Models" → kebab "models-list" → strip "models-" → "list"
	op := &operationInfo{
		Tag:         "Models",
		OperationID: "ModelsList",
		Method:      "GET",
		Path:        "/api/v1/models",
	}
	if got := commandName(op); got != "list" {
		t.Errorf("commandName (with tag prefix) = %q, want %q", got, "list")
	}

	// Without operationID → falls back to "method-lastPathSegment"
	op2 := &operationInfo{
		Tag:    "misc",
		Method: "GET",
		Path:   "/api/v1/widgets",
	}
	if got := commandName(op2); got != "get-widgets" {
		t.Errorf("commandName (no operationID) = %q, want %q", got, "get-widgets")
	}

	// Path ending in {param} → skips the param placeholder and uses the
	// resource name before it, e.g. DELETE /widgets/{widgetId} → "delete-widgets"
	op3 := &operationInfo{
		Tag:    "misc",
		Method: "DELETE",
		Path:   "/api/v1/widgets/{widgetId}",
	}
	if got := commandName(op3); got != "delete-widgets" {
		t.Errorf("commandName (path param) = %q, want %q", got, "delete-widgets")
	}
}

// deprecatedMsg returns a message string for deprecated operations (shown by
// cobra when --help is used) or empty string for non-deprecated ones.
func TestDeprecatedMsg(t *testing.T) {
	if msg := deprecatedMsg(&operationInfo{Deprecated: true}); msg == "" {
		t.Error("deprecatedMsg(true) should return a non-empty string")
	}
	if msg := deprecatedMsg(&operationInfo{Deprecated: false}); msg != "" {
		t.Errorf("deprecatedMsg(false) = %q, want empty", msg)
	}
}

// ---------------------------------------------------------------------------
// readStdin limit
//
// readStdin caps input at 10 MB. Verify it rejects oversized input.
// ---------------------------------------------------------------------------

func TestReadStdin_RejectsOversized(t *testing.T) {
	// Create a reader that's 1 byte over the limit
	oversize := make([]byte, maxStdinSize+1)
	origStdin := os.Stdin

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r

	go func() {
		w.Write(oversize)
		w.Close()
	}()

	_, err = readStdin()
	os.Stdin = origStdin

	if err == nil {
		t.Fatal("expected error for oversized stdin, got nil")
	}
	if !strings.Contains(err.Error(), "10 MB") {
		t.Errorf("error = %q, want it to mention 10 MB", err.Error())
	}
}

func TestReadStdin_AcceptsWithinLimit(t *testing.T) {
	data := []byte(`{"key":"value"}`)
	origStdin := os.Stdin

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r

	go func() {
		w.Write(data)
		w.Close()
	}()

	got, err := readStdin()
	os.Stdin = origStdin

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", string(got), string(data))
	}
}

// ---------------------------------------------------------------------------
// Command generation from real spec
//
// These tests load the actual api/openapi.json file (the same spec that gets
// embedded into the CLI binary) and verify that GenerateCommands produces
// the expected command tree structure.
// ---------------------------------------------------------------------------

// loadSpec reads the real OpenAPI spec from disk. Tests that use this verify
// the CLI will work with the actual spec, not just a synthetic test fixture.
func loadSpec(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	return data
}

// TestGenerateCommandsFromSpec is a smoke test: feed the real OpenAPI spec
// into GenerateCommands and verify it produces a non-empty command tree.
// Each API tag (like "Models", "Documents", "AI") becomes a parent command,
// and each operation under that tag becomes a subcommand.
func TestGenerateCommandsFromSpec(t *testing.T) {
	specData := loadSpec(t)

	// The executor is never actually called here — we're just checking that
	// the spec parses and produces commands, not executing them.
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands(specData, noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	if len(cmds) == 0 {
		t.Fatal("expected at least one tag group command")
	}

	// Count all subcommands (individual API operations) across all tags.
	total := 0
	for _, c := range cmds {
		total += len(c.Commands())
	}
	t.Logf("Generated %d tag groups with %d total subcommands", len(cmds), total)
	if total == 0 {
		t.Fatal("expected subcommands")
	}
}

// Some endpoints declare path params in the spec's parameters array in a
// different order than they appear in the URL (e.g. the v2 document draft
// routes list draftIdentifier before identifier). Positional args must follow
// the URL shape, so a generated command's arg order matches the path template
// regardless of declaration order.
func TestGenerateCommands_PathParamsFollowPathOrder(t *testing.T) {
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "test", "version": "1.0"},
		"paths": {
			"/api/v1/things/{outerId}/items/{innerId}": {
				"get": {
					"operationId": "testGetItem",
					"tags": ["test"],
					"parameters": [
						{"name": "innerId", "in": "path", "required": true, "schema": {"type": "string"}},
						{"name": "outerId", "in": "path", "required": true, "schema": {"type": "string"}}
					],
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`

	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }
	cmds, err := GenerateCommands([]byte(spec), exec)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	if len(cmds) != 1 || len(cmds[0].Commands()) != 1 {
		t.Fatalf("expected 1 tag group with 1 subcommand, got %v", cmds)
	}

	sub := cmds[0].Commands()[0]
	if sub.Use != "get-item <outer-id> <inner-id>" {
		t.Errorf("Use = %q, want %q", sub.Use, "get-item <outer-id> <inner-id>")
	}

	// Positional args in path order must substitute into the matching slots.
	if err := sub.RunE(sub, []string{"outer-val", "inner-val"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if captured.Path != "/api/v1/things/outer-val/items/inner-val" {
		t.Errorf("path = %q, want %q", captured.Path, "/api/v1/things/outer-val/items/inner-val")
	}
}

// ---------------------------------------------------------------------------
// Command behavior tests
//
// These use buildCommand directly with hand-crafted operationInfo structs
// (not the real spec) to test specific behaviors: path parameter substitution,
// query flag handling, body passing, and argument validation.
//
// The pattern: we pass a "recording" executor that saves the APIRequest it
// receives, then inspect that captured request to verify the command wired
// things up correctly.
// ---------------------------------------------------------------------------

// Verify that path parameters (like {orgId} and {widgetId} in the URL) are
// replaced with the positional arguments the user passes on the command line.
// e.g. "omni models get-view myModel myView" should produce the API path
// "/api/v1/models/myModel/view/myView".
func TestBuildCommand_PathParams(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testGetWidget",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/widgets/{widgetId}",
		PathParams: []paramInfo{
			{Name: "orgId", In: "path"},
			{Name: "widgetId", In: "path"},
		},
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{}) // clear; we call RunE directly
	if err := cmd.RunE(cmd, []string{"org-123", "w-456"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if captured.Path != "/api/v1/orgs/org-123/widgets/w-456" {
		t.Errorf("path = %q, want substituted path", captured.Path)
	}
	if captured.Method != "GET" {
		t.Errorf("method = %q, want GET", captured.Method)
	}
}

// Verify that query parameters defined in the spec become CLI flags, and
// their values get appended to the URL as a query string.
// e.g. "omni content list --page-size 50 --cursor abc" should produce
// "?cursor=abc&page_size=50" on the API path.
func TestBuildCommand_QueryFlags(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testListItems",
		Method:      "GET",
		Path:        "/api/v1/items",
		QueryParams: []paramInfo{
			{Name: "page_size", In: "query"},
			{Name: "cursor", In: "query"},
		},
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"--page-size", "50", "--cursor", "abc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(captured.Path, "page_size=50") {
		t.Errorf("path %q missing page_size=50", captured.Path)
	}
	if !strings.Contains(captured.Path, "cursor=abc") {
		t.Errorf("path %q missing cursor=abc", captured.Path)
	}
}

// ---------------------------------------------------------------------------
// Required query params + the generic --query escape hatch
// ---------------------------------------------------------------------------

// listOp is a GET operation with one required query param and one optional one.
func listOp() *operationInfo {
	return &operationInfo{
		Tag:         "test",
		OperationID: "testListItems",
		Method:      "GET",
		Path:        "/api/v1/items",
		QueryParams: []paramInfo{
			{Name: "connectionId", In: "query", Required: true},
			{Name: "page_size", In: "query"},
		},
	}
}

// A query param the spec marks required must fail client-side when it's
// missing, instead of costing a round trip and a server 400.
func TestBuildCommand_RequiredQueryParamMissing(t *testing.T) {
	called := false
	exec := func(req APIRequest) error { called = true; return nil }

	cmd := buildCommand(listOp(), exec)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--page-size", "10"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing required query param, got nil")
	}
	if !strings.Contains(err.Error(), "connection-id") {
		t.Errorf("error = %q, want it to name the missing flag", err.Error())
	}
	if called {
		t.Error("executor should not run when a required flag is missing")
	}

	// The flag's usage should advertise that it's required.
	usage := cmd.Flags().Lookup("connection-id").Usage
	if !strings.Contains(usage, "(required)") {
		t.Errorf("usage = %q, want it to mention (required)", usage)
	}
}

func TestBuildCommand_RequiredQueryParamPresent(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	cmd := buildCommand(listOp(), exec)
	cmd.SetArgs([]string{"--connectionid", "c-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured.Path, "connectionId=c-1") {
		t.Errorf("path %q missing connectionId=c-1", captured.Path)
	}
}

// MarkFlagRequired only proves the flag was supplied, so an explicit empty
// value (--connectionid=) would otherwise sail past validation and then be
// dropped from the query string — the server 400 this is meant to prevent.
func TestBuildCommand_RequiredQueryParamEmpty(t *testing.T) {
	cases := [][]string{
		{"--connectionid="},                      // explicit empty declared flag
		{"--connectionid", ""},                   // same, separate-arg form
		{"--connectionid=", "--page-size", "10"}, // empty alongside other params
	}

	for _, args := range cases {
		called := false
		exec := func(req APIRequest) error { called = true; return nil }

		cmd := buildCommand(listOp(), exec)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(args)

		err := cmd.Execute()
		if err == nil {
			t.Fatalf("args %v: expected an error for an empty required param, got nil", args)
		}
		if !strings.Contains(err.Error(), "connection-id") {
			t.Errorf("args %v: error = %q, want it to name the flag", args, err.Error())
		}
		if called {
			t.Errorf("args %v: executor should not run for an empty required param", args)
		}
	}
}

// The emptiness check runs on the assembled query string, so it also covers
// values that arrived through the --query escape hatch (under either the spec
// spelling or the flag spelling).
func TestCheckRequiredQueryParams(t *testing.T) {
	op := listOp()
	cases := []struct {
		name    string
		query   url.Values
		wantErr bool
	}{
		{"absent", url.Values{}, true},
		{"empty under spec name", url.Values{"connectionId": {""}}, true},
		{"empty under flag name", url.Values{"connectionid": {""}}, true},
		{"set under spec name", url.Values{"connectionId": {"c-1"}}, false},
		{"set under flag name", url.Values{"connectionid": {"c-1"}}, false},
		{"one of several non-empty", url.Values{"connectionId": {"", "c-1"}}, false},
		{"optional param empty is fine", url.Values{"connectionId": {"c-1"}, "page_size": {""}}, false},
	}

	for _, c := range cases {
		err := checkRequiredQueryParams(resolveQueryFlags(op), c.query)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got nil", c.name)
			} else if !strings.Contains(err.Error(), "connection-id") {
				t.Errorf("%s: error = %q, want it to name the flag", c.name, err.Error())
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
	}
}

// A declared required param has its own flag, so --query is not a substitute
// for it: cobra still insists the flag itself be supplied.
func TestBuildCommand_ExtraQueryDoesNotSubstituteForRequiredFlag(t *testing.T) {
	called := false
	exec := func(req APIRequest) error { called = true; return nil }

	cmd := buildCommand(listOp(), exec)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--query", "connectionId=c-1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected the required flag to still be enforced, got nil")
	}
	if !strings.Contains(err.Error(), "connection-id") {
		t.Errorf("error = %q, want it to name the missing flag", err.Error())
	}
	if called {
		t.Error("executor should not run when the required flag is missing")
	}
}

// --query is the escape hatch for params the spec doesn't declare. It's
// repeatable, and repeating a key sends every value.
func TestBuildCommand_ExtraQueryParams(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	cmd := buildCommand(listOp(), exec)
	cmd.SetArgs([]string{
		"--connectionid", "c-1",
		"--query", "undeclared=yes",
		"--query", "tag=a",
		"--query", "tag=b",
		"--query", "empty=",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"connectionId=c-1", "undeclared=yes", "tag=a&tag=b", "empty="} {
		if !strings.Contains(captured.Path, want) {
			t.Errorf("path %q missing %q", captured.Path, want)
		}
	}
}

func TestBuildCommand_ExtraQueryParamMalformed(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	for _, bad := range []string{"noequals", "=novalue"} {
		cmd := buildCommand(listOp(), exec)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs([]string{"--connectionid", "c-1", "--query", bad})

		err := cmd.Execute()
		if err == nil {
			t.Fatalf("--query %q: expected an error, got nil", bad)
		}
		if !strings.Contains(err.Error(), "key=value") {
			t.Errorf("--query %q: error = %q, want it to explain key=value", bad, err.Error())
		}
	}
}

// Sending the same param both ways is ambiguous — error rather than silently
// picking one. Both the spec spelling and the flag spelling are caught.
func TestBuildCommand_ExtraQueryParamConflict(t *testing.T) {
	for _, key := range []string{"connectionId", "connectionid", "connection-id"} {
		called := false
		exec := func(req APIRequest) error { called = true; return nil }

		cmd := buildCommand(listOp(), exec)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs([]string{"--connectionid", "c-1", "--query", key + "=c-2"})

		err := cmd.Execute()
		if err == nil {
			t.Fatalf("--query %s=: expected a conflict error, got nil", key)
		}
		if !strings.Contains(err.Error(), "conflicts with --connection-id") {
			t.Errorf("error = %q, want it to name the conflicting flag", err.Error())
		}
		if called {
			t.Error("executor should not run on a conflicting --query")
		}
	}
}

// An unset declared flag isn't a conflict — --query can supply its value.
func TestBuildCommand_ExtraQueryParamNoConflictWhenFlagUnset(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	cmd := buildCommand(listOp(), exec)
	cmd.SetArgs([]string{"--connectionid", "c-1", "--query", "page_size=25"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured.Path, "page_size=25") {
		t.Errorf("path %q missing page_size=25", captured.Path)
	}
}

// A spec param that slugifies to "query" owns the flag name; the escape hatch
// steps aside rather than panicking the flag registration.
func TestBuildCommand_QueryParamNamedQuery(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testSearch",
		Method:      "GET",
		Path:        "/api/v1/search",
		QueryParams: []paramInfo{{Name: "query", In: "query"}},
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"--query", "revenue"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured.Path, "query=revenue") {
		t.Errorf("path %q missing query=revenue", captured.Path)
	}
}

// --schema is local discovery with no API call, so it must stay zero-friction
// even on an operation with required query params.
func TestBuildCommand_SchemaIgnoresRequiredQueryParams(t *testing.T) {
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "test", "version": "1.0"},
		"paths": {
			"/api/v1/widgets": {
				"post": {
					"operationId": "widgetsCreate",
					"tags": ["widgets"],
					"parameters": [
						{"name": "connectionId", "in": "query", "required": true, "schema": {"type": "string"}}
					],
					"requestBody": {
						"content": {"application/json": {"schema": {
							"type": "object",
							"required": ["name"],
							"properties": {"name": {"type": "string"}}
						}}}
					},
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`

	called := false
	// Each case gets a fresh command tree; cobra keeps parsed flag state on the
	// command, and the real CLI runs one command per process.
	run := func(args ...string) (string, error) {
		exec := func(req APIRequest) error { called = true; return nil }
		cmds, err := GenerateCommands([]byte(spec), exec)
		if err != nil {
			t.Fatalf("GenerateCommands: %v", err)
		}
		group := cmds[0]
		var buf bytes.Buffer
		group.SetOut(&buf)
		group.SetErr(&buf)
		group.SilenceUsage = true
		group.SilenceErrors = true
		group.SetArgs(args)
		execErr := group.Execute()
		return buf.String(), execErr
	}

	out, err := run("create", "--schema")
	if err != nil {
		t.Fatalf("Execute --schema: %v\n%s", err, out)
	}
	if called {
		t.Error("--schema must not make an API call")
	}
	if !strings.Contains(out, `"name"`) {
		t.Errorf("schema output missing body fields: %s", out)
	}

	// Without --schema the required param is still enforced.
	if _, err := run("create", "--body", "{}"); err == nil {
		t.Fatal("expected a missing-required-flag error without --schema")
	}

	// ...and supplying it goes through.
	if out, err := run("create", "--body", "{}", "--connectionid", "c-1"); err != nil {
		t.Fatalf("Execute with required param: %v\n%s", err, out)
	}
	if !called {
		t.Error("expected the API call to run once the required param was set")
	}
}

// Verify that operations with a request body (POST/PUT/PATCH) get a --body
// flag, and the JSON value passed to it ends up in APIRequest.Body.
func TestBuildCommand_BodyFlag(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testCreateWidget",
		Method:      "POST",
		Path:        "/api/v1/widgets",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"--body", `{"key":"val"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if string(captured.Body) != `{"key":"val"}` {
		t.Errorf("body = %q, want {\"key\":\"val\"}", string(captured.Body))
	}
}

// Verify that cobra rejects commands when the user provides the wrong number
// of positional arguments. If an endpoint requires a {widgetId} path param,
// the user must provide exactly 1 arg.
func TestBuildCommand_WrongArgCount(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testGetWidget",
		Method:      "GET",
		Path:        "/api/v1/widgets/{widgetId}",
		PathParams: []paramInfo{
			{Name: "widgetId", In: "path"},
		},
	}

	cmd := buildCommand(op, exec)
	// Silence usage/error output during test
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{}) // 0 args, expects 1
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for wrong arg count, got nil")
	}
}

// A command that fails at runtime (e.g. the API returned HTTP 400) should not
// print the usage block after the error — the message is the useful part, and
// the usage text is noise that buries it.
func TestBuildCommand_RuntimeErrorSilencesUsage(t *testing.T) {
	exec := func(req APIRequest) error { return fmt.Errorf("API returned HTTP 400") }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testListItems",
		Method:      "GET",
		Path:        "/api/v1/items",
	}

	var out bytes.Buffer
	cmd := buildCommand(op, exec)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected the executor error to propagate")
	}
	if !cmd.SilenceUsage {
		t.Error("SilenceUsage should be set once RunE is entered")
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("usage block should be suppressed, got %q", out.String())
	}
}

// Flag-parse errors happen before RunE, so they're genuine usage errors and
// should still print the usage block.
func TestBuildCommand_FlagErrorKeepsUsage(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testListItems",
		Method:      "GET",
		Path:        "/api/v1/items",
	}

	var out bytes.Buffer
	cmd := buildCommand(op, exec)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--nope"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("usage block should be printed for flag errors, got %q", out.String())
	}
}

// newTestGroup builds a tag group shaped like the ones GenerateCommands makes,
// hung off a root command so cobra routes args to the group the way it does in
// the real binary.
func newTestGroup(out, errOut *bytes.Buffer) (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "omni"}
	root.SilenceErrors = true
	group := NewGroupCommand("models", "models commands")
	group.AddCommand(&cobra.Command{Use: "list", Short: "list models", RunE: func(*cobra.Command, []string) error { return nil }})
	group.AddCommand(&cobra.Command{Use: "get <id>", Short: "get a model", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(group)
	root.SetOut(out)
	root.SetErr(errOut)
	return root, group
}

// A mistyped subcommand must be a hard error with suggestions — not a silent
// help dump on stdout that a piped consumer would try to parse as data.
func TestGroupRunE_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root, group := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models", "list-branches"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if !strings.Contains(err.Error(), `unknown subcommand "list-branches" for "omni models"`) {
		t.Errorf("error = %q, want an unknown-subcommand message", err.Error())
	}
	// "list-branches" is too far from "list" for Levenshtein, but the
	// reverse-prefix pass should still point there.
	if !strings.Contains(err.Error(), "Did you mean this?") || !strings.Contains(err.Error(), "list") {
		t.Errorf("error = %q, want a suggestion for 'list'", err.Error())
	}
	if !strings.Contains(err.Error(), "omni models --help") {
		t.Errorf("error = %q, want a --help hint", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
	if !group.SilenceUsage {
		t.Error("SilenceUsage should be set so the error isn't buried in usage text")
	}
}

// Nothing similar to suggest: still an error, still nothing on stdout.
func TestGroupRunE_UnknownSubcommandNoSuggestions(t *testing.T) {
	var out, errOut bytes.Buffer
	root, _ := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models", "zzzzzzzz"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if strings.Contains(err.Error(), "Did you mean this?") {
		t.Errorf("error = %q, want no suggestion block", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
}

// A bare group produced no data, so its help goes to stderr and the exit code
// is non-zero — stdout stays empty for the pipe.
func TestGroupRunE_NoSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root, _ := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected a non-nil error so the CLI exits non-zero")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Errorf("error = %q, want a 'requires a subcommand' message", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Available Commands") {
		t.Errorf("stderr = %q, want the group help", errOut.String())
	}
}

// The bare-group help IS the error report, so cobra must not append its own
// "Error: ..." line on top of it — one failure, one surface.
func TestGroupRunE_NoSubcommandReportsErrorOnce(t *testing.T) {
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "omni"}
	group := NewGroupCommand("models", "models commands")
	group.AddCommand(&cobra.Command{Use: "list", Short: "list models", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(group)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"models"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected a non-nil error so the CLI exits non-zero")
	}
	if strings.Contains(errOut.String(), "Error:") {
		t.Errorf("stderr = %q, want no duplicate cobra error line alongside the help", errOut.String())
	}
	if n := strings.Count(errOut.String(), "Usage:"); n != 1 {
		t.Errorf("stderr has %d usage blocks, want exactly 1: %q", n, errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
}

// --help is not an error: it still prints to stdout and exits zero.
func TestGroupRunE_HelpFlagStaysOnStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	root, _ := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	if !strings.Contains(out.String(), "Available Commands") {
		t.Errorf("stdout = %q, want the group help", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", errOut.String())
	}
}

// A typo plus --help is still a typo. Cobra answers the help flag before RunE,
// so this is the one path that could still print help to stdout and exit 0.
func TestGroupHelp_UnknownSubcommandWithHelpFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	root, group := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models", "list-branches", "--help"})

	// Cobra always returns nil once it has handled the help flag, which is why
	// the caller has to consult UnknownSubcommand for the exit code.
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error from Execute: %v", err)
	}
	if !UnknownSubcommand(group) {
		t.Error("UnknownSubcommand should report the typo so the CLI exits non-zero")
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), `unknown subcommand "list-branches"`) {
		t.Errorf("stderr = %q, want an unknown-subcommand error", errOut.String())
	}
	if !strings.Contains(errOut.String(), "Did you mean this?") {
		t.Errorf("stderr = %q, want a suggestion", errOut.String())
	}
}

// The group's help func is inherited by its subcommands, so `--help` on a real
// subcommand must still render normal help on stdout (and not recurse).
func TestGroupHelp_SubcommandHelpUnaffected(t *testing.T) {
	var out, errOut bytes.Buffer
	root, group := newTestGroup(&out, &errOut)
	root.SetArgs([]string{"models", "list", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	if UnknownSubcommand(group) {
		t.Error("a valid subcommand should not be reported as unknown")
	}
	if !strings.Contains(out.String(), "list models") || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("stdout = %q, want the subcommand's help", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", errOut.String())
	}
}

// Every generated group gets the unknown-subcommand handling, not just the
// ones someone remembered to wire up.
func TestGenerateCommands_GroupsAreRunnable(t *testing.T) {
	specData := loadSpec(t)
	cmds, err := GenerateCommands(specData, func(req APIRequest) error { return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	for _, tagCmd := range cmds {
		if tagCmd.RunE == nil {
			t.Errorf("group %q has no RunE; an unknown subcommand would exit 0", tagCmd.Use)
		}
		if !isGroup(tagCmd) {
			t.Errorf("group %q is missing the group annotation; --help after a typo would exit 0", tagCmd.Use)
		}
	}
}

// Every generated command must accept --schema (and its --field/--depth
// refinements), including bodyless GET/DELETE ones. Agents reach for --schema
// first, so a command missing the flag costs a wasted call.
func TestGenerateCommands_SchemaFlagOnEveryCommand(t *testing.T) {
	specData := loadSpec(t)
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands(specData, noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	// A spec param may legitimately own one of these names, in which case the
	// discovery flag lives under its fallback — but it must exist either way.
	fallbacks := map[string]string{"schema": "schema-doc", "field": "schema-field", "depth": "schema-depth"}

	checked := 0
	for _, tagCmd := range cmds {
		for _, sub := range tagCmd.Commands() {
			for flag, fallback := range fallbacks {
				if sub.Flags().Lookup(flag) == nil && sub.Flags().Lookup(fallback) == nil {
					t.Errorf("%s %s: neither --%s nor --%s registered", tagCmd.Name(), sub.Name(), flag, fallback)
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no subcommands were checked")
	}
	t.Logf("checked --schema registration on %d subcommands", checked)
}

// freeFlagName must never give up: when both the preferred name and its
// fallback are taken it keeps suffixing until it finds a free one, so the
// discovery flag is always registered somewhere.
func TestFreeFlagName(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	if got := freeFlagName(cmd, "schema", "schema-doc"); got != "schema" {
		t.Errorf("freeFlagName on an empty command = %q, want schema", got)
	}

	cmd.Flags().String("schema", "", "a spec param")
	if got := freeFlagName(cmd, "schema", "schema-doc"); got != "schema-doc" {
		t.Errorf("freeFlagName with schema taken = %q, want schema-doc", got)
	}

	cmd.Flags().String("schema-doc", "", "another spec param")
	if got := freeFlagName(cmd, "schema", "schema-doc"); got != "schema-doc-2" {
		t.Errorf("freeFlagName with both taken = %q, want schema-doc-2", got)
	}
}

// --schema must short-circuit before positional-arg validation, so a command
// with required path params still answers without them (and without a token).
func TestBuildCommand_SchemaSkipsArgValidation(t *testing.T) {
	var called bool
	exec := func(req APIRequest) error { called = true; return nil }

	op := &operationInfo{
		Tag:         "test",
		OperationID: "testGetWidget",
		Method:      "GET",
		Path:        "/api/v1/widgets/{widgetId}",
		PathParams:  []paramInfo{{Name: "widgetId", In: "path", Type: "string"}},
	}

	cmd := buildCommand(op, exec)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--schema"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --schema with no args: %v", err)
	}
	if called {
		t.Error("executor was called on a --schema run")
	}
	if !strings.Contains(buf.String(), `"body": null`) {
		t.Errorf("schema output missing an explicit null body: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Flag-name normalization
//
// The spec spells the same concept differently across sibling operations
// ("branchId" on models validate, "branch_id" on models list-topics). Flag
// names are derived by splitting camelCase, so both become --branch-id, and
// lookups ignore case and dash/underscore placement so older spellings keep
// working.
// ---------------------------------------------------------------------------

// canonicalName turns an OpenAPI param name into the kebab-case flag name.
func TestCanonicalName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"branchId", "branch-id"},
		{"branch_id", "branch-id"},
		{"branch-id", "branch-id"},
		{"pageSize", "page-size"},
		{"modelKind", "model-kind"},
		{"baseModelId", "base-model-id"},
		{"cursor", "cursor"},
		{"id", "id"},
		{"", ""},
		// Acronym runs stay together rather than exploding letter by letter.
		{"modelURL", "model-url"},
		{"URLPrefix", "url-prefix"},
		{"parseJSONBody", "parse-json-body"},
		// A pluralized acronym stays one word instead of splitting on the "s".
		{"URLs", "urls"},
		{"IDs", "ids"},
		{"apiURLs", "api-urls"},
		{"modelIDs", "model-ids"},
		{"IDsList", "ids-list"},
		// Digits attach to the word they follow.
		{"v2Identifier", "v2-identifier"},
	}
	for _, c := range cases {
		if got := canonicalName(c.in); got != c.want {
			t.Errorf("canonicalName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// flagLookupKey is the equivalence class used to match a typed flag name
// against the registered one.
func TestFlagLookupKey(t *testing.T) {
	same := []string{"branch-id", "branchId", "branch_id", "branchid", "BRANCH-ID"}
	want := flagLookupKey(same[0])
	for _, s := range same {
		if got := flagLookupKey(s); got != want {
			t.Errorf("flagLookupKey(%q) = %q, want %q", s, got, want)
		}
	}
	if flagLookupKey("branch-id") == flagLookupKey("branch-ids") {
		t.Error("distinct flag names must not share a lookup key")
	}
}

// A camelCase query param registers a kebab-case flag, and that canonical
// spelling is the only one --help advertises.
func TestBuildCommand_CamelCaseQueryParamBecomesKebabFlag(t *testing.T) {
	op := &operationInfo{
		Tag:         "models",
		OperationID: "modelsValidate",
		Method:      "GET",
		Path:        "/api/v1/models/validate",
		QueryParams: []paramInfo{{Name: "branchId", In: "query"}},
	}

	cmd := buildCommand(op, func(req APIRequest) error { return nil })

	f := cmd.Flags().Lookup("branch-id")
	if f == nil {
		t.Fatal("expected a --branch-id flag to be registered")
	}
	if f.Name != "branch-id" {
		t.Errorf("registered flag name = %q, want %q", f.Name, "branch-id")
	}
	if usage := cmd.Flags().FlagUsages(); !strings.Contains(usage, "--branch-id") || strings.Contains(usage, "--branchid") {
		t.Errorf("help output should offer only the canonical spelling, got:\n%s", usage)
	}
}

// Every spelling that differs only in case or separators resolves to the same
// flag, and the value is sent under the param's ORIGINAL spec name.
func TestBuildCommand_AlternateFlagSpellings(t *testing.T) {
	for _, spelling := range []string{"--branch-id", "--branchid", "--branchId", "--branch_id", "--BranchID"} {
		t.Run(spelling, func(t *testing.T) {
			var captured APIRequest
			op := &operationInfo{
				Tag:         "models",
				OperationID: "modelsValidate",
				Method:      "GET",
				Path:        "/api/v1/models/validate",
				QueryParams: []paramInfo{{Name: "branchId", In: "query"}},
			}
			cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetArgs([]string{spelling, "br-123"})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute(%s): %v", spelling, err)
			}
			// The query string uses the spec's spelling, not the flag's.
			if captured.Path != "/api/v1/models/validate?branchId=br-123" {
				t.Errorf("path = %q, want branchId=br-123 in the query string", captured.Path)
			}
		})
	}
}

// The drift this fixes: sibling commands whose spec params differ only in
// naming style now answer to one flag spelling, in both directions.
func TestGenerateCommands_SiblingParamsAgreeOnFlagName(t *testing.T) {
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "test", "version": "1.0"},
		"paths": {
			"/api/v1/models/validate": {
				"get": {
					"operationId": "modelsValidate",
					"tags": ["models"],
					"parameters": [{"name": "branchId", "in": "query", "schema": {"type": "string"}}],
					"responses": {"200": {"description": "ok"}}
				}
			},
			"/api/v1/models/topics": {
				"get": {
					"operationId": "modelsListTopics",
					"tags": ["models"],
					"parameters": [{"name": "branch_id", "in": "query", "schema": {"type": "string"}}],
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`

	cmds, err := GenerateCommands([]byte(spec), func(req APIRequest) error { return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	for _, sub := range cmds[0].Commands() {
		for _, spelling := range []string{"branch-id", "branchid", "branchId"} {
			if sub.Flags().Lookup(spelling) == nil {
				t.Errorf("%s: --%s does not resolve to a registered flag", sub.Name(), spelling)
			}
		}
	}
}

// Two params on one operation that normalize identically cannot share a flag
// (pflag panics on the second), but the spec is synced from upstream, so the
// later one is renamed out of the way rather than failing generation — which
// would take down every command in the CLI, not just this one.
func TestGenerateCommands_DegradesOnFlagCollision(t *testing.T) {
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "test", "version": "1.0"},
		"paths": {
			"/api/v1/things": {
				"get": {
					"operationId": "thingsList",
					"tags": ["things"],
					"parameters": [
						{"name": "branchId", "in": "query", "schema": {"type": "string"}},
						{"name": "branch_id", "in": "query", "schema": {"type": "string"}}
					],
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`

	var captured APIRequest
	cmds, err := GenerateCommands([]byte(spec), func(req APIRequest) error { captured = req; return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	var cmd *cobra.Command
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == "list" {
			cmd = sub
		}
	}
	if cmd == nil {
		t.Fatal("expected a generated \"things list\" command")
	}
	if cmd.Flags().Lookup("branch-id") == nil {
		t.Fatal("the first param should keep the canonical --branch-id")
	}
	second := cmd.Flags().Lookup("branch-id-2")
	if second == nil {
		t.Fatal("the second param should be registered under a fallback name")
	}
	// The rename is announced, otherwise --branch-id-2 looks like a typo.
	if !strings.Contains(second.Usage, `"branch_id"`) {
		t.Errorf("fallback flag usage should name the original param, got %q", second.Usage)
	}

	// Both flags still reach the server under their own spec spellings.
	tagCmd := cmds[0]
	tagCmd.SilenceUsage, tagCmd.SilenceErrors = true, true
	tagCmd.SetArgs([]string{"list", "--branch-id", "a", "--branch-id-2", "b"})
	if err := tagCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured.Path, "branchId=a") || !strings.Contains(captured.Path, "branch_id=b") {
		t.Errorf("path = %q, want both branchId=a and branch_id=b", captured.Path)
	}
}

func TestValidateFlagNames(t *testing.T) {
	// Distinct params are fine, even when one is camelCase and one snake_case.
	ok := &operationInfo{
		OperationID: "thingsList",
		QueryParams: []paramInfo{{Name: "branchId"}, {Name: "page_size"}, {Name: "cursor"}},
	}
	if err := validateFlagNames(ok); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Params that canonicalize alike are renamed by resolveQueryFlags, so they
	// are not reported either.
	colliding := &operationInfo{
		OperationID: "thingsList",
		QueryParams: []paramInfo{{Name: "branchId"}, {Name: "branch_id"}},
	}
	if err := validateFlagNames(colliding); err != nil {
		t.Errorf("unexpected error for colliding params: %v", err)
	}

	// A param whose name matches a built-in flag is renamed out of the way
	// rather than reported, so it is not a collision.
	withBody := &operationInfo{
		OperationID: "thingsCreate",
		QueryParams: []paramInfo{{Name: "Body"}},
		HasBody:     true,
	}
	if err := validateFlagNames(withBody); err != nil {
		t.Errorf("unexpected error for a reserved-name param: %v", err)
	}

	// A param colliding with a promoted body-shorthand flag is renamed too —
	// labelsCreate promotes "color" and "description" — so nothing is reported.
	withShorthand := &operationInfo{
		OperationID: "labelsCreate",
		QueryParams: []paramInfo{{Name: "Color"}},
		HasBody:     true,
	}
	if err := validateFlagNames(withShorthand); err != nil {
		t.Errorf("unexpected error for a param colliding with a shorthand: %v", err)
	}

	// Only hand-written shorthands are reported: one that took a built-in
	// flag's name could not be registered at all.
	sh := GetBodyShorthand("labelsCreate")
	original := sh.Flags
	sh.Flags = append([]FlagMapping{{FlagName: "body", FieldPath: "name"}}, original...)
	defer func() { sh.Flags = original }()
	if err := validateFlagNames(withShorthand); err == nil {
		t.Error("expected a shorthand named after a built-in flag to be reported")
	}
}

// Promoted body-shorthand flags are registered with pflag's Var (their type
// comes from the request schema), not String. Registration still goes through
// the normalization function, so alternate spellings must reach them too — and
// the schema-derived typing has to survive the trip.
func TestShorthand_TypedFlagsAcceptAlternateSpellings(t *testing.T) {
	op := operationFromRealSpec(t, "aiGenerateQuery")

	// Sanity: the flag is boolean-typed from the spec, not a plain string.
	probe := buildCommand(op, func(req APIRequest) error { return nil })
	runQuery := probe.Flags().Lookup("run-query")
	if runQuery == nil {
		t.Fatal("expected a --run-query flag")
	}
	if runQuery.Value.Type() != "boolean" {
		t.Fatalf("--run-query type = %q, want %q", runQuery.Value.Type(), "boolean")
	}

	for _, spelling := range []string{"--run-query", "--runquery", "--runQuery", "--run_query"} {
		t.Run(spelling, func(t *testing.T) {
			var captured APIRequest
			cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetArgs([]string{"m-1", "how many orders", spelling, "false", "--userid", "u-1"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute(%s): %v", spelling, err)
			}

			var body map[string]interface{}
			if err := json.Unmarshal(captured.Body, &body); err != nil {
				t.Fatalf("unmarshaling body: %v", err)
			}
			// Boolean typing survives: a JSON false, not the string "false".
			if v, ok := body["runQuery"].(bool); !ok || v {
				t.Errorf("body[runQuery] = %#v, want false (bool)", body["runQuery"])
			}
			if body["userId"] != "u-1" {
				t.Errorf("body[userId] = %#v, want %q", body["userId"], "u-1")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Reserved (global / built-in) flag names
//
// Normalization widened what counts as a collision: a spec param named
// "baseUrl" used to slugify to the harmless --baseurl, but canonicalizes to
// --base-url. Registered locally it would win the name — pflag's AddFlagSet
// skips a persistent flag whose name is already taken — and resolveConfig's
// GetString("base-url") would then read the query param's value, letting one
// value both filter the request and choose the host it goes to.
// ---------------------------------------------------------------------------

func TestResolveQueryFlags_ReservedNamesArePrefixed(t *testing.T) {
	cases := []struct {
		in      string
		hasBody bool
		want    string
	}{
		// Global flags: reserved on every command.
		{"baseUrl", false, "param-base-url"},
		{"base_url", false, "param-base-url"},
		{"token", false, "param-token"},
		{"profile", false, "param-profile"},
		{"compact", false, "param-compact"},
		{"format", false, "param-format"},
		{"help", false, "param-help"},
		// Body flags: only registered — and so only reserved — on operations
		// that take a request body.
		{"body", true, "param-body"},
		{"schema", true, "param-schema"},
		{"field", true, "param-field"},
		{"depth", true, "param-depth"},
		{"body", false, "body"},
		{"schema", false, "schema"},
		{"field", false, "field"},
		{"depth", false, "depth"},
		// Everything else keeps its canonical name.
		{"branchId", true, "branch-id"},
		{"pageSize", false, "page-size"},
		{"baseModelId", false, "base-model-id"},
		{"formatting", false, "formatting"},
		{"tokenId", false, "token-id"},
	}
	for _, c := range cases {
		op := &operationInfo{
			OperationID: "thingsList",
			QueryParams: []paramInfo{{Name: c.in, In: "query"}},
			HasBody:     c.hasBody,
		}
		if got := resolveQueryFlags(op)[0].Name; got != c.want {
			t.Errorf("param %q (hasBody=%v) registered as --%s, want --%s", c.in, c.hasBody, got, c.want)
		}
	}
}

// A body shorthand is our hand-written UX sugar; the query param is the
// endpoint's contract. When a synced spec adds a param that lands on a
// shorthand's flag name, the param moves aside — generation must not fail, that
// would take down the whole CLI.
func TestResolveQueryFlags_ShorthandNamesAreReservedPerOperation(t *testing.T) {
	// labelsCreate promotes "color" and "description" as shorthand flags.
	collides := &operationInfo{
		OperationID: "labelsCreate",
		QueryParams: []paramInfo{{Name: "color", In: "query"}},
		HasBody:     true,
	}
	got := resolveQueryFlags(collides)[0]
	if got.Name != "param-color" {
		t.Errorf("param %q registered as --%s, want --param-color", "color", got.Name)
	}
	if !strings.Contains(got.Note, "shorthand") {
		t.Errorf("help note should explain the shorthand collision, got %q", got.Note)
	}

	// The same param name on an operation with no shorthand is left alone.
	free := &operationInfo{
		OperationID: "thingsList",
		QueryParams: []paramInfo{{Name: "color", In: "query"}},
	}
	if name := resolveQueryFlags(free)[0].Name; name != "color" {
		t.Errorf("without a shorthand the param should keep --color, got --%s", name)
	}
}

// End to end: a synced spec that adds a query param named after one of our
// shorthand flags still generates, both flags work, and the param goes out
// under its spec spelling.
func TestGenerateCommands_QueryParamCollidingWithShorthand(t *testing.T) {
	// labelsCreate promotes --color as a body shorthand; the spec here also
	// declares a "color" query param.
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "test", "version": "1.0"},
		"paths": {
			"/api/v1/labels": {
				"post": {
					"operationId": "labelsCreate",
					"tags": ["labels"],
					"parameters": [{"name": "color", "in": "query", "schema": {"type": "string"}}],
					"requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {
						"name": {"type": "string"},
						"color": {"type": "string"}
					}}}}},
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`

	var captured APIRequest
	cmds, err := GenerateCommands([]byte(spec), func(req APIRequest) error { captured = req; return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	var cmd *cobra.Command
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == "create" {
			cmd = sub
		}
	}
	if cmd == nil {
		t.Fatal("expected a generated \"labels create\" command")
	}

	// The shorthand keeps --color; the query param moves to --param-color.
	if f := cmd.Flags().Lookup("color"); f == nil || f.Usage != "hex color (e.g. #0366d6)"+bodyExclusiveSuffix {
		t.Fatalf("--color should still be the body shorthand, got %#v", f)
	}
	param := cmd.Flags().Lookup("param-color")
	if param == nil {
		t.Fatal("expected the query param to be registered as --param-color")
	}
	if !strings.Contains(param.Usage, "shorthand") {
		t.Errorf("help should explain the rename, got %q", param.Usage)
	}

	tagCmd := cmds[0]
	tagCmd.SilenceUsage, tagCmd.SilenceErrors = true, true
	tagCmd.SetArgs([]string{"create", "important", "--color", "#0366d6", "--param-color", "red"})
	if err := tagCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The query param goes out under its spec spelling...
	if captured.Path != "/api/v1/labels?color=red" {
		t.Errorf("path = %q, want the param sent as color=red", captured.Path)
	}
	// ...and the shorthand still lands in the body.
	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshaling body: %v", err)
	}
	if body["color"] != "#0366d6" || body["name"] != "important" {
		t.Errorf("body = %#v, want name=important and color=#0366d6", body)
	}
}

// A bodyless operation registers no --field, so a param of that name keeps it
// instead of being renamed on the strength of a flag that is not there.
func TestBuildCommand_BodylessOperationDoesNotReserveBodyFlags(t *testing.T) {
	op := &operationInfo{
		Tag:         "things",
		OperationID: "thingsList",
		Method:      "GET",
		Path:        "/api/v1/things",
		QueryParams: []paramInfo{{Name: "field", In: "query"}},
	}
	cmd := buildCommand(op, func(req APIRequest) error { return nil })

	f := cmd.Flags().Lookup("field")
	if f == nil {
		t.Fatal("expected the param to keep --field on a bodyless command")
	}
	if strings.Contains(f.Usage, "renamed") {
		t.Errorf("help should not claim a rename, got %q", f.Usage)
	}
	if cmd.Flags().Lookup("param-field") != nil {
		t.Error("--param-field should not exist on a bodyless command")
	}
}

func TestIsReservedFlagName_IgnoresSpelling(t *testing.T) {
	for _, name := range []string{"base-url", "baseUrl", "base_url", "BASEURL", "help", "FORMAT"} {
		if !IsReservedFlagName(name) {
			t.Errorf("IsReservedFlagName(%q) = false, want true", name)
		}
	}
	// Flags only some commands register are not globally reserved.
	for _, name := range []string{"branch-id", "page-size", "param-base-url", "tokens", "json-body", "field"} {
		if IsReservedFlagName(name) {
			t.Errorf("IsReservedFlagName(%q) = true, want false", name)
		}
	}
}

// A spec param that would shadow a global flag is registered under --param-,
// the global name is left alone, and the value still goes out under the
// param's original spec spelling.
func TestBuildCommand_ReservedQueryParamDoesNotShadowGlobalFlag(t *testing.T) {
	var captured APIRequest
	op := &operationInfo{
		Tag:         "things",
		OperationID: "thingsList",
		Method:      "GET",
		Path:        "/api/v1/things",
		QueryParams: []paramInfo{
			{Name: "baseUrl", In: "query", Description: "filter by callback base URL"},
			{Name: "cursor", In: "query"},
		},
	}
	cmd := buildCommand(op, func(req APIRequest) error { captured = req; return nil })

	// The command must not define --base-url itself; that name belongs to the
	// root's persistent flag, which merges in at parse time.
	if f := cmd.Flags().Lookup("base-url"); f != nil {
		t.Fatalf("generated command registered a local --%s, shadowing the global flag", f.Name)
	}
	if cmd.Flags().Lookup("param-base-url") == nil {
		t.Fatal("expected the param to be registered as --param-base-url")
	}
	if usage := cmd.Flags().FlagUsages(); !strings.Contains(usage, `"baseUrl"`) {
		t.Errorf("help should name the original spec param, got:\n%s", usage)
	}

	// A real root command with the global flag, so parsing mirrors production.
	root := &cobra.Command{Use: "omni"}
	root.PersistentFlags().String("base-url", "", "API base URL (overrides profile)")
	root.SetGlobalNormalizationFunc(NormalizeFlagName)
	root.AddCommand(cmd)
	root.SetArgs([]string{"list", "--param-base-url", "https://filter.example", "--base-url", "https://api.example"})
	root.SilenceUsage, root.SilenceErrors = true, true
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if captured.Path != "/api/v1/things?baseUrl=https%3A%2F%2Ffilter.example" {
		t.Errorf("path = %q, want the param sent as baseUrl", captured.Path)
	}
	// The global flag still holds the value the user gave it.
	if got, _ := cmd.Flags().GetString("base-url"); got != "https://api.example" {
		t.Errorf("--base-url = %q, want the global value %q", got, "https://api.example")
	}
}

// Command names and flag names share one slug renderer (canonicalName).
// Unifying them was safe only because it changes no command name the real spec
// generates — unlike flags, a command spelling has no normalization fallback,
// so a change would break every user script that calls it. This guards the
// property: names stay unique within their group and free of the letter-by-
// letter acronym splits the old renderer produced ("get-u-r-l-info").
func TestRealSpec_CommandNamesAreStable(t *testing.T) {
	specData := loadSpec(t)
	doc, err := libopenapi.NewDocument(specData)
	if err != nil {
		t.Fatalf("parsing spec: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("building model: %v", err)
	}

	groups := map[string][]*operationInfo{}
	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		extractOperations(pair.Key(), pair.Value(), groups)
	}
	if len(groups) == 0 {
		t.Fatal("no operations parsed from the spec")
	}

	for tag, ops := range groups {
		seen := map[string]string{}
		for _, op := range ops {
			name := commandName(op)
			if name == "" {
				t.Errorf("%s: operation %s produced an empty command name", tag, op.OperationID)
				continue
			}
			for _, seg := range strings.Split(name, "-") {
				if len(seg) == 1 {
					t.Errorf("%s %s: single-letter segment %q suggests an acronym was split letter by letter", slugify(tag), name, seg)
				}
			}
			if prev, ok := seen[name]; ok {
				t.Errorf("%s: %s and %s both generate the command %q", slugify(tag), prev, op.OperationID, name)
			}
			seen[name] = op.OperationID
		}
	}
}

// The real spec must generate cleanly. Any param that had to be renamed — to
// avoid a global flag or another param on the same operation — is logged, since
// that is a spec change worth noticing on a sync.
func TestRealSpec_NoFlagCollisions(t *testing.T) {
	specData := loadSpec(t)
	specOps := parseSpecOperations(t, specData)
	if len(specOps) == 0 {
		t.Fatal("no operations parsed from the spec")
	}

	doc, err := libopenapi.NewDocument(specData)
	if err != nil {
		t.Fatalf("parsing spec: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("building model: %v", err)
	}

	groups := map[string][]*operationInfo{}
	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		extractOperations(pair.Key(), pair.Value(), groups)
	}

	renamed := 0
	for tag, ops := range groups {
		for _, op := range ops {
			if err := validateFlagNames(op); err != nil {
				t.Errorf("%s %s: %v", slugify(tag), commandName(op), err)
			}
			for _, qf := range resolveQueryFlags(op) {
				if qf.Note != "" {
					renamed++
					t.Logf("%s %s: param %q registered as --%s %s", slugify(tag), commandName(op), qf.Param.Name, qf.Name, qf.Note)
				}
			}
		}
	}
	t.Logf("%d query params renamed to avoid a flag name that was already taken", renamed)
}

// ---------------------------------------------------------------------------
// Spec coverage reporter
//
// This is the centerpiece test. It answers: "does every API endpoint in our
// OpenAPI spec actually produce a working CLI command?"
//
// How it works:
//   1. Parse the OpenAPI spec ourselves to get a list of ALL operations
//      (e.g. GET /api/v1/models, POST /api/v1/documents, etc.)
//   2. Run GenerateCommands (the same code path the real CLI uses)
//   3. For every generated subcommand, invoke it with dummy arguments and
//      a no-op executor (no real HTTP calls)
//   4. Track which operations executed successfully
//   5. Print a per-tag coverage table and fail if anything is missing
//
// If someone adds a new endpoint to the OpenAPI spec and the code generator
// can't handle it (e.g. a new parameter type), this test will catch it.
// ---------------------------------------------------------------------------

// specOperation holds data we parse from the spec independently of the
// command generator, so we can cross-reference what the generator produced.
type specOperation struct {
	Tag           string
	OperationID   string
	Method        string
	Path          string
	PathParams    []string
	RequiredQuery []string // flag names of required query params
	HasBody       bool
	BodyMedia     string
}

// parseSpecOperations reads the OpenAPI spec directly (bypassing our generator)
// to get the ground-truth list of every API operation. We use this to verify
// that GenerateCommands didn't silently drop any endpoints.
func parseSpecOperations(t *testing.T, specData []byte) []specOperation {
	t.Helper()

	doc, err := libopenapi.NewDocument(specData)
	if err != nil {
		t.Fatalf("parsing spec: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("building model: %v", err)
	}

	var ops []specOperation
	if model.Model.Paths == nil || model.Model.Paths.PathItems == nil {
		return ops
	}

	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		pathStr := pair.Key()
		pathItem := pair.Value()

		methods := map[string]*v3.Operation{
			"GET":    pathItem.Get,
			"POST":   pathItem.Post,
			"PUT":    pathItem.Put,
			"DELETE": pathItem.Delete,
			"PATCH":  pathItem.Patch,
		}

		for method, op := range methods {
			if op == nil {
				continue
			}
			tag := "misc"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}

			var pathParams, requiredQuery []string
			for _, p := range op.Parameters {
				switch p.In {
				case "path":
					pathParams = append(pathParams, p.Name)
				case "query":
					if boolVal(p.Required) {
						requiredQuery = append(requiredQuery, slugify(p.Name))
					}
				}
			}

			ops = append(ops, specOperation{
				Tag:           tag,
				OperationID:   op.OperationId,
				Method:        method,
				Path:          pathStr,
				PathParams:    pathParams,
				RequiredQuery: requiredQuery,
				HasBody:       op.RequestBody != nil,
				BodyMedia:     specBodyMediaType(op.RequestBody),
			})
		}
	}
	return ops
}

// specBodyMediaType names the media type the generator will pick for a request
// body, so the coverage table records what each operation is sent as.
func specBodyMediaType(rb *v3.RequestBody) string {
	mediaType, _ := requestBodyMediaType(rb)
	return mediaType
}

func TestSpecCoverage(t *testing.T) {
	specData := loadSpec(t)
	specOps := parseSpecOperations(t, specData)

	// Build a lookup table so we can match each generated cobra subcommand
	// back to the spec operation it came from. The key is "tag-slug/cmd-name",
	// e.g. "models/list" or "ai/generate-query". We compute this by running
	// the same commandName() and slugify() functions the generator uses.
	keyToOp := map[string]specOperation{}
	for _, sop := range specOps {
		info := &operationInfo{
			Tag:         sop.Tag,
			OperationID: sop.OperationID,
			Method:      sop.Method,
			Path:        sop.Path,
		}
		cmdName := commandName(info)
		tagSlug := slugify(sop.Tag)
		key := tagSlug + "/" + cmdName
		keyToOp[key] = sop
	}

	// Generate all CLI commands from the spec, just like the real binary does
	// at startup. The executor is a no-op — we're testing command generation
	// and argument wiring, not HTTP requests.
	called := map[string]bool{}
	exec := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands(specData, exec)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	// Walk the generated command tree and try to execute every subcommand.
	// For each one, we supply dummy positional args ("test-id" for each path
	// parameter) and an empty JSON body where needed. If RunE succeeds, the
	// operation is marked as covered.
	var failures []string
	for _, tagCmd := range cmds {
		for _, sub := range tagCmd.Commands() {
			key := tagCmd.Use + "/" + sub.Name()
			sop, ok := keyToOp[key]
			if !ok {
				failures = append(failures, fmt.Sprintf("no spec mapping for %s", key))
				continue
			}

			// Build dummy positional args — one per path parameter.
			// e.g. "omni models get-view <modelId> <viewName>" needs 2 args.
			args := make([]string, len(sop.PathParams))
			for i := range args {
				args[i] = "test-id"
			}

			// Required query params must carry a non-empty value, same as they
			// would on a real invocation.
			for _, flagName := range sop.RequiredQuery {
				if err := sub.Flags().Set(flagName, "test-value"); err != nil {
					failures = append(failures, fmt.Sprintf("%s: set required query flag %s: %v", key, flagName, err))
					continue
				}
			}

			// Operations with a request body (POST/PUT/PATCH) need --body set,
			// otherwise the command would try to read from stdin.
			if sop.HasBody {
				if err := sub.Flags().Set("body", "{}"); err != nil {
					failures = append(failures, fmt.Sprintf("%s: set body flag: %v", key, err))
					continue
				}
			}

			// Query params the spec marks required are enforced client-side, so
			// give each one a dummy value — otherwise the command could never
			// reach RunE and the operation would read as uncovered.
			setErr := ""
			sub.Flags().VisitAll(func(f *pflag.Flag) {
				ann := f.Annotations[cobra.BashCompOneRequiredFlag]
				if setErr != "" || len(ann) == 0 || ann[0] != "true" {
					return
				}
				if err := sub.Flags().Set(f.Name, "test-value"); err != nil {
					setErr = fmt.Sprintf("%s: set %s flag: %v", key, f.Name, err)
				}
			})
			if setErr != "" {
				failures = append(failures, setErr)
				continue
			}

			if sub.RunE == nil {
				failures = append(failures, fmt.Sprintf("%s: no RunE", key))
				continue
			}

			if err := sub.RunE(sub, args); err != nil {
				failures = append(failures, fmt.Sprintf("%s: RunE: %v", key, err))
				continue
			}
			called[sop.OperationID] = true
		}
	}

	// Build and print the coverage report. This is the output you see when
	// running "go test -v -run TestSpecCoverage ./internal/openapi/"
	type tagStats struct {
		covered int
		total   int
	}
	tagMap := map[string]*tagStats{}
	for _, sop := range specOps {
		tag := sop.Tag
		if tagMap[tag] == nil {
			tagMap[tag] = &tagStats{}
		}
		tagMap[tag].total++
		if called[sop.OperationID] {
			tagMap[tag].covered++
		}
	}

	tags := make([]string, 0, len(tagMap))
	for tag := range tagMap {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	t.Logf("")
	t.Logf("--- OpenAPI Spec Coverage ---")
	t.Logf("%-25s %8s %6s %6s", "Tag", "Covered", "Total", "Pct")
	totalCovered, totalOps := 0, 0
	for _, tag := range tags {
		s := tagMap[tag]
		pct := 0
		if s.total > 0 {
			pct = s.covered * 100 / s.total
		}
		t.Logf("%-25s %8d %6d %5d%%", tag, s.covered, s.total, pct)
		totalCovered += s.covered
		totalOps += s.total
	}
	pct := 0
	if totalOps > 0 {
		pct = totalCovered * 100 / totalOps
	}
	t.Logf("%-25s %8d %6d %5d%%", "TOTAL", totalCovered, totalOps, pct)

	// List uncovered operations
	var uncovered []string
	for _, sop := range specOps {
		if !called[sop.OperationID] {
			uncovered = append(uncovered, fmt.Sprintf("%s %s (%s)", sop.Method, sop.Path, sop.OperationID))
		}
	}
	if len(uncovered) > 0 {
		t.Logf("")
		t.Logf("Uncovered operations:")
		for _, u := range uncovered {
			t.Logf("  %s", u)
		}
	}

	// Report failures
	for _, f := range failures {
		t.Errorf("FAIL: %s", f)
	}

	if len(uncovered) > 0 {
		t.Errorf("%d/%d operations uncovered", len(uncovered), totalOps)
	}
}

// ---------------------------------------------------------------------------
// Arguments help section tests
//
// Cobra's usage line only shows placeholders like "<branchname>", which is how
// an agent ends up passing a branch UUID to a command that wants a branch NAME.
// buildCommand therefore renders the spec's param descriptions into an
// "Arguments:" block in the command's long help.
// ---------------------------------------------------------------------------

func TestBuildCommand_ArgumentsSection(t *testing.T) {
	op := &operationInfo{
		Tag:         "Models",
		OperationID: "modelsMergeBranch",
		Summary:     "Merge branch",
		Method:      "POST",
		Path:        "/api/v1/models/{modelId}/branch/{branchName}/merge",
		PathParams: []paramInfo{
			{Name: "modelId", In: "path", Description: "Model UUID"},
			{Name: "branchName", In: "path", Description: "Branch name"},
		},
	}

	cmd := buildCommand(op, func(req APIRequest) error { return nil })
	if !strings.Contains(cmd.Long, "Arguments:") {
		t.Fatalf("Long = %q, expected an Arguments section", cmd.Long)
	}
	// The names must match the usage line's spelling, which is canonical
	// kebab-case — "<modelid>" here next to "<model-id>" there reads as two
	// different arguments.
	for _, want := range []string{"<model-id>", "Model UUID", "<branch-name>", "Branch name"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long = %q, expected it to contain %q", cmd.Long, want)
		}
	}
	// With no Description on the operation, the Short still has to survive.
	if !strings.Contains(cmd.Long, "Merge branch") {
		t.Errorf("Long = %q, expected the summary to be preserved", cmd.Long)
	}
}

// Shorthand positional args are part of the same positional list, so they
// belong in the same block as the path params.
func TestBuildCommand_ArgumentsSectionIncludesShorthandArgs(t *testing.T) {
	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiGenerateQuery",
		Method:      "POST",
		Path:        "/api/v1/ai/generate-query",
		HasBody:     true,
	}

	cmd := buildCommand(op, func(req APIRequest) error { return nil })
	for _, want := range []string{"Arguments:", "<model-id>", "UUID of the shared model", "<prompt>", "natural language query prompt"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long = %q, expected it to contain %q", cmd.Long, want)
		}
	}
}

// Commands with no positionals must not grow an empty Arguments block.
func TestBuildCommand_NoArgumentsSectionWithoutPositionals(t *testing.T) {
	op := &operationInfo{
		Tag:         "Models",
		OperationID: "modelsList",
		Summary:     "List models",
		Method:      "GET",
		Path:        "/api/v1/models",
	}

	cmd := buildCommand(op, func(req APIRequest) error { return nil })
	if strings.Contains(cmd.Long, "Arguments:") {
		t.Errorf("Long = %q, expected no Arguments section", cmd.Long)
	}
}

// End-to-end against the real spec: the description the spec gives for a path
// param has to reach the generated command's help.
func TestGenerateCommands_ArgumentsSectionFromSpec(t *testing.T) {
	specData := loadSpec(t)
	cmds, err := GenerateCommands(specData, func(req APIRequest) error { return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	var merge *cobra.Command
	for _, tagCmd := range cmds {
		if tagCmd.Use != "models" {
			continue
		}
		for _, sub := range tagCmd.Commands() {
			if sub.Name() == "merge-branch" {
				merge = sub
			}
		}
	}
	if merge == nil {
		t.Fatal("models merge-branch not found in generated commands")
	}

	// The second positional is a branch NAME, not a branch UUID — that
	// distinction is exactly what the Arguments section exists to convey.
	if !strings.Contains(merge.Long, "Arguments:") {
		t.Fatalf("Long = %q, expected an Arguments section", merge.Long)
	}
	if !strings.Contains(merge.Long, "Branch name") {
		t.Errorf("Long = %q, expected the spec's branchName description", merge.Long)
	}
}

// The Arguments block and the usage line describe the same positionals, so a
// reader comparing them must see the same names. They are built from separate
// code paths, which is exactly how they drifted before ("<modelid>" in the
// Arguments block, "<model-id>" in the usage line).
func TestGenerateCommands_ArgumentNamesMatchUsageLine(t *testing.T) {
	specData := loadSpec(t)
	cmds, err := GenerateCommands(specData, func(req APIRequest) error { return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}

	placeholder := regexp.MustCompile(`<[^>]+>`)
	checked := 0
	for _, tagCmd := range cmds {
		for _, sub := range tagCmd.Commands() {
			names := placeholder.FindAllString(sub.Use, -1)
			if len(names) == 0 {
				continue
			}
			checked++
			for _, name := range names {
				if !strings.Contains(sub.Long, "\n  "+name) {
					t.Errorf("%s %s: usage line has %s but the Arguments block does not:\n%s",
						tagCmd.Use, sub.Name(), name, sub.Long)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no commands with positional args found; the spec or generator changed")
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Model UUID", "Model UUID"},
		{"  padded  ", "padded"},
		{"first line\nsecond line", "first line"},
		{"", ""},
	}
	for _, c := range cases {
		if got := firstLine(c.in); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
