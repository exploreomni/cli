package openapi

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/spf13/cobra"
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

// camelToKebab converts operationIds like "aiJobStatus" into kebab-case
// "ai-job-status" for use as CLI subcommand names.
func TestCamelToKebab(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ModelsList", "models-list"},
		{"aiJobStatus", "ai-job-status"},
		{"a", "a"},
		{"ABC", "a-b-c"},
	}
	for _, c := range cases {
		if got := camelToKebab(c.in); got != c.want {
			t.Errorf("camelToKebab(%q) = %q, want %q", c.in, got, c.want)
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
	if sub.Use != "get-item <outerid> <innerid>" {
		t.Errorf("Use = %q, want %q", sub.Use, "get-item <outerid> <innerid>")
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
	Tag         string
	OperationID string
	Method      string
	Path        string
	PathParams  []string
	HasBody     bool
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

			var pathParams []string
			for _, p := range op.Parameters {
				if p.In == "path" {
					pathParams = append(pathParams, p.Name)
				}
			}

			ops = append(ops, specOperation{
				Tag:         tag,
				OperationID: op.OperationId,
				Method:      method,
				Path:        pathStr,
				PathParams:  pathParams,
				HasBody:     op.RequestBody != nil,
			})
		}
	}
	return ops
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

			// Operations with a request body (POST/PUT/PATCH) need --body set,
			// otherwise the command would try to read from stdin.
			if sop.HasBody {
				if err := sub.Flags().Set("body", "{}"); err != nil {
					failures = append(failures, fmt.Sprintf("%s: set body flag: %v", key, err))
					continue
				}
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
