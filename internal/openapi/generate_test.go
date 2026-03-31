package openapi

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
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
