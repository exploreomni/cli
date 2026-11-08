package openapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// validationSpec exercises every rule ValidateBody applies. Each schema under
// components is reached through bodySchema(t, name).
const validationSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "t", "version": "1"},
  "paths": {},
  "components": {"schemas": {
    "Strict": {
      "type": "object",
      "required": ["name", "pageSize"],
      "properties": {
        "name": {"type": "string", "minLength": 1, "maxLength": 5},
        "pageSize": {"type": "integer", "minimum": 1, "maximum": 100},
        "ratio": {"type": "number", "exclusiveMinimum": 0},
        "mode": {"type": "string", "enum": ["fast", "slow"]},
        "id": {"type": "string", "format": "uuid"},
        "slug": {"type": "string", "pattern": "^[a-z-]+$"},
        "note": {"type": ["string", "null"]},
        "tags": {"type": "array", "minItems": 1, "maxItems": 2, "items": {"type": "string"}},
        "rows": {"type": "array", "items": {"type": "object", "required": ["k"], "properties": {"k": {"type": "string"}}}},
        "createdAt": {"type": "string", "readOnly": true}
      }
    },
    "Open": {
      "type": "object",
      "properties": {"a": {"type": "string"}},
      "additionalProperties": true
    },
    "Typed": {
      "type": "object",
      "properties": {"a": {"type": "string"}},
      "additionalProperties": {"type": "number"}
    },
    "Free": {"type": "object"},
    "Closed": {
      "type": "object",
      "properties": {"a": {"type": "string"}},
      "additionalProperties": false
    },
    "Composed": {
      "allOf": [
        {"type": "object", "required": ["name"], "properties": {"name": {"type": "string"}}},
        {"type": "object", "properties": {"color": {"type": "string"}}}
      ]
    },
    "Delivery": {
      "allOf": [
        {"type": "object", "required": ["name"], "properties": {"name": {"type": "string"}}},
        {"oneOf": [
          {"type": "object", "required": ["destinationType", "recipients"], "properties": {
            "destinationType": {"type": "string", "enum": ["email"]},
            "recipients": {"type": "array", "items": {"type": "string"}}
          }},
          {"type": "object", "required": ["destinationType", "url"], "properties": {
            "destinationType": {"type": "string", "enum": ["webhook"]},
            "url": {"type": "string"}
          }}
        ]}
      ]
    },
    "NullableRef": {
      "type": "object",
      "properties": {"inner": {"anyOf": [{"$ref": "#/components/schemas/Closed"}, {"type": "null"}]}}
    },
    "Overlap": {
      "oneOf": [
        {"type": "object", "properties": {"a": {"type": "string"}}, "additionalProperties": true},
        {"type": "object", "properties": {"a": {"type": "string"}}, "additionalProperties": true}
      ]
    },
    "Exclusive": {
      "allOf": [
        {"not": {"required": ["app", "containers"]}},
        {"type": "object", "properties": {
          "app": {"type": "object"},
          "containers": {"type": "object"},
          "branch": {"type": "string", "not": {"pattern": "[\\r\\n]"}}
        }}
      ]
    }
  }}
}`

func bodySchema(t *testing.T, name string) *base.SchemaProxy {
	t.Helper()
	doc, err := libopenapi.NewDocument([]byte(validationSpec))
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatal(err)
	}
	proxy, ok := model.Model.Components.Schemas.Get(name)
	if !ok {
		t.Fatalf("schema %q not found", name)
	}
	return proxy
}

// violations runs ValidateBody and renders each violation as "path: message".
func violations(t *testing.T, schema, body string) []string {
	t.Helper()
	var out []string
	for _, v := range ValidateBody(bodySchema(t, schema), []byte(body)) {
		out = append(out, v.Path+": "+v.Message)
	}
	return out
}

func expectViolations(t *testing.T, schema, body string, want ...string) {
	t.Helper()
	got := violations(t, schema, body)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ValidateBody(%s, %s)\n got: %q\nwant: %q", schema, body, got, want)
	}
}

func TestValidateBody_ValidBodyPasses(t *testing.T) {
	expectViolations(t, "Strict", `{
		"name": "abc", "pageSize": 10, "ratio": 0.5, "mode": "fast",
		"id": "123e4567-e89b-12d3-a456-426614174000", "slug": "a-b", "note": null,
		"tags": ["x"], "rows": [{"k": "v"}]
	}`)
}

// The point of reporting everything at once: one round of fixes, not one per
// problem.
func TestValidateBody_ReportsEveryViolationSortedByPath(t *testing.T) {
	expectViolations(t, "Strict", `{"pagesize": 10, "mode": "medium", "tags": [], "extra": 1}`,
		`body.extra: unknown field "extra"`,
		`body.mode: must be one of "fast", "slow", got "medium"`,
		`body.name: missing required field "name"`,
		`body.pageSize: missing required field "pageSize"`,
		`body.pagesize: unknown field "pagesize" (did you mean "pageSize"?)`,
		`body.tags: must have at least 1 items, got 0`,
	)
}

func TestValidateBody_SuggestsNearestName(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "slgu": "x", "page_size": 2}`,
		`body.page_size: unknown field "page_size" (did you mean "pageSize"?)`,
		`body.slgu: unknown field "slgu" (did you mean "slug"?)`,
	)
	// Nothing close: no suggestion rather than a misleading one.
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "workbookUrl": true}`,
		`body.workbookUrl: unknown field "workbookUrl"`)
}

func TestValidateBody_EnumSuggestion(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "mode": "Fast"}`,
		`body.mode: must be one of "fast", "slow", got "Fast" (did you mean "fast"?)`)
}

func TestValidateBody_Types(t *testing.T) {
	expectViolations(t, "Strict", `{"name": 5, "pageSize": 2.5, "tags": "x", "note": 3}`,
		`body.name: expected string, got integer`,
		`body.note: expected string or null, got integer`,
		`body.pageSize: expected integer, got number`,
		`body.tags: expected array, got string`,
	)
	// A whole number is a valid "number", and 10.0 is a valid "integer".
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 10.0, "ratio": 3}`)
	expectViolations(t, "Strict", `[1]`, `body: expected object, got array`)
}

func TestValidateBody_Bounds(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "", "pageSize": 101, "ratio": 0, "tags": ["a", "b", "c"]}`,
		`body.name: must be at least 1 characters, got 0`,
		`body.pageSize: must be at most 100, got 101`,
		`body.ratio: must be greater than 0, got 0`,
		`body.tags: must have at most 2 items, got 3`,
	)
	expectViolations(t, "Strict", `{"name": "abcdef", "pageSize": 0}`,
		`body.name: must be 5 characters or fewer, got 6`,
		`body.pageSize: must be at least 1, got 0`,
	)
}

// An emoji is one code point but two UTF-16 units. The API (JavaScript) and
// JSON Schema disagree on its length, so the check takes the reading that
// lets the value through: five emoji fit a maxLength of 5.
func TestValidateBody_LengthNeverStricterThanEitherReading(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "😀😀😀😀😀", "pageSize": 1}`)
}

func TestValidateBody_FormatAndPattern(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "id": "not-a-uuid", "slug": "Bad Slug"}`,
		`body.id: must be a UUID, got "not-a-uuid"`,
		`body.slug: must match pattern ^[a-z-]+$`,
	)
}

func TestValidateBody_NestedArrayPaths(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "rows": [{"k": "x"}, {"key": "y"}]}`,
		`body.rows[1].k: missing required field "k"`,
		`body.rows[1].key: unknown field "key" (did you mean "k"?)`,
	)
}

// Read-only fields are what a caller gets by echoing a GET response into a
// create or update.
func TestValidateBody_ReadOnly(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1, "createdAt": "2026-01-01"}`,
		`body.createdAt: "createdAt" is read-only; the API sets it, so leave it out of the request`)
}

func TestValidateBody_AdditionalProperties(t *testing.T) {
	expectViolations(t, "Open", `{"a": "x", "anything": [1, 2]}`)
	expectViolations(t, "Typed", `{"a": "x", "n": 1, "m": "two"}`,
		`body.m: expected number, got string`)
	expectViolations(t, "Free", `{"whatever": {"deep": true}}`)
	expectViolations(t, "Closed", `{"b": 1}`, `body.b: unknown field "b"`)
}

// allOf members contribute keys to one object: a key declared by either
// member is known, and each member's required list still applies.
func TestValidateBody_AllOfMergesProperties(t *testing.T) {
	expectViolations(t, "Composed", `{"name": "n", "color": "red"}`)
	expectViolations(t, "Composed", `{"colour": "red"}`,
		`body.colour: unknown field "colour" (did you mean "color"?)`,
		`body.name: missing required field "name"`,
	)
}

// The schedules shape: shared fields in one allOf member, destination fields in
// a oneOf. Branch keys must not be called unknown, and a body that picks a
// branch by its discriminator is held to that branch.
func TestValidateBody_UnionInsideAllOf(t *testing.T) {
	expectViolations(t, "Delivery", `{"name": "n", "destinationType": "email", "recipients": ["a@b.c"]}`)
	expectViolations(t, "Delivery", `{"name": "n", "destinationType": "webhook", "url": "https://x"}`)
	expectViolations(t, "Delivery", `{"name": "n", "destinationType": "webhook", "recipients": ["a@b.c"]}`,
		`body.recipients: unknown field "recipients"`,
		`body.url: missing required field "url"`,
	)
	expectViolations(t, "Delivery", `{"name": "n", "destinationType": "email", "recipients": [], "subjct": "hi"}`,
		`body.subjct: unknown field "subjct"`)
}

func TestValidateBody_NullableRef(t *testing.T) {
	expectViolations(t, "NullableRef", `{"inner": null}`)
	expectViolations(t, "NullableRef", `{"inner": {"a": "x"}}`)
	expectViolations(t, "NullableRef", `{"inner": {"b": "x"}}`,
		`body.inner.b: unknown field "b"`)
	expectViolations(t, "NullableRef", `{"inner": 3}`,
		`body.inner: expected object or null, got integer`)
}

// oneOf is checked like anyOf: a value matching two overlapping branches is a
// spec imprecision, not the caller's mistake.
func TestValidateBody_OverlappingOneOfIsNotAnError(t *testing.T) {
	expectViolations(t, "Overlap", `{"a": "x"}`)
}

func TestValidateBody_Not(t *testing.T) {
	expectViolations(t, "Exclusive", `{"app": {}}`)
	expectViolations(t, "Exclusive", `{"app": {}, "containers": {}}`,
		`body: "app" and "containers" cannot be used together`)
	expectViolations(t, "Exclusive", `{"branch": "a\nb"}`,
		`body.branch: must not match pattern [\r\n]`)
}

func TestValidateBody_DottedKeysAreQuoted(t *testing.T) {
	expectViolations(t, "Typed", `{"orders.status": "x"}`,
		`body["orders.status"]: expected number, got string`)
}

func TestValidateBody_InvalidJSON(t *testing.T) {
	got := violations(t, "Strict", `{"name": }`)
	if len(got) != 1 || !strings.HasPrefix(got[0], "body: not valid JSON") {
		t.Errorf("got %q, want one invalid-JSON violation", got)
	}
}

// A number too large for float64 is still judged as a number, not dropped or
// misreported.
func TestValidateBody_LargeNumbers(t *testing.T) {
	expectViolations(t, "Strict", `{"name": "a", "pageSize": 1e400}`,
		`body.pageSize: expected integer, got number`)
}

// ---------------------------------------------------------------------------
// --dry-run on generated commands
// ---------------------------------------------------------------------------

const dryRunSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "t", "version": "1"},
  "paths": {
    "/things": {
      "post": {
        "operationId": "thingsCreate",
        "tags": ["things"],
        "parameters": [{"name": "mode", "in": "query", "schema": {"type": "string", "enum": ["draft", "live"]}}],
        "requestBody": {"required": true, "content": {"application/json": {"schema": {
          "type": "object", "required": ["name"],
          "properties": {"name": {"type": "string"}, "pageSize": {"type": "integer"}}
        }}}},
        "responses": {"200": {"description": "ok"}}
      },
      "get": {
        "operationId": "thingsList",
        "tags": ["things"],
        "parameters": [{"name": "dryRun", "in": "query", "schema": {"type": "boolean"}}],
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

// runDryRun executes a generated command and reports what it wrote and whether
// the executor (the network) was reached.
func runDryRun(t *testing.T, name string, args ...string) (stdout, stderr string, err error, sent bool) {
	t.Helper()
	exec := func(APIRequest) error { sent = true; return nil }
	cmds, gerr := GenerateCommands([]byte(dryRunSpec), exec)
	if gerr != nil {
		t.Fatalf("GenerateCommands: %v", gerr)
	}
	group := cmds[0]
	var out, errBuf bytes.Buffer
	group.SetOut(&out)
	group.SetErr(&errBuf)
	group.SilenceUsage = true
	group.SetArgs(append([]string{name}, args...))
	err = group.Execute()
	return out.String(), errBuf.String(), err, sent
}

func TestDryRun_ValidRequestPrintsResultAndSendsNothing(t *testing.T) {
	stdout, stderr, err, sent := runDryRun(t, "create", "--dry-run", "--mode", "live", "--body", `{"name":"x"}`)
	if err != nil || sent {
		t.Fatalf("err=%v sent=%v stderr=%s", err, sent, stderr)
	}
	var res DryRunResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not a result document: %v\n%s", err, stdout)
	}
	want := DryRunResult{Valid: true, Method: "POST", Path: "/things?mode=live", BodyChecked: true}
	if res != want {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestDryRun_InvalidRequestReportsOneDocumentOnStderr(t *testing.T) {
	stdout, stderr, err, sent := runDryRun(t, "create", "--dry-run", "--mode", "liev", "--body", `{"nmae":"x","pageSize":"10"}`)
	if err == nil {
		t.Fatal("expected a non-nil error so the command exits non-zero")
	}
	if sent {
		t.Fatal("a failed --dry-run must not reach the executor")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", stdout)
	}
	var doc DryRunError
	if err := json.Unmarshal([]byte(stderr), &doc); err != nil {
		t.Fatalf("stderr is not exactly one JSON document: %v\n%s", err, stderr)
	}
	want := []Violation{
		{Path: "--mode", Message: `must be one of draft, live, got "liev" (did you mean "live"?)`},
		{Path: "body.name", Message: `missing required field "name"`},
		{Path: "body.nmae", Message: `unknown field "nmae" (did you mean "name"?)`},
		{Path: "body.pageSize", Message: "expected integer, got string"},
	}
	if len(doc.Violations) != len(want) {
		t.Fatalf("violations = %+v, want %+v", doc.Violations, want)
	}
	for i := range want {
		if doc.Violations[i] != want[i] {
			t.Errorf("violation %d = %+v, want %+v", i, doc.Violations[i], want[i])
		}
	}
	if doc.Error != "request failed validation (4 problems); nothing was sent" {
		t.Errorf("error = %q", doc.Error)
	}
}

func TestDryRun_MissingRequiredBody(t *testing.T) {
	_, stderr, err, sent := runDryRun(t, "create", "--dry-run")
	if err == nil || sent {
		t.Fatalf("err=%v sent=%v", err, sent)
	}
	if !strings.Contains(stderr, `"path": "body"`) || !strings.Contains(stderr, "request body is required") {
		t.Errorf("stderr = %s", stderr)
	}
}

// Without the flag nothing changes: the request goes out unvalidated, exactly
// as before, so a stale embedded spec can never block a call by default.
func TestDryRun_OffByDefault(t *testing.T) {
	_, _, err, sent := runDryRun(t, "create", "--body", `{"nmae":"x"}`)
	if err != nil || !sent {
		t.Fatalf("err=%v sent=%v; want the request sent unvalidated", err, sent)
	}
}

// A spec parameter named dryRun keeps --dry-run; the discovery flag falls back
// so it is never unavailable and the parameter is never shadowed.
func TestDryRun_FlagFallsBackWhenAParameterOwnsTheName(t *testing.T) {
	stdout, _, err, sent := runDryRun(t, "list", "--validate-only", "--dry-run", "true")
	if err != nil || sent {
		t.Fatalf("err=%v sent=%v", err, sent)
	}
	if !strings.Contains(stdout, `"path": "/things?dryRun=true"`) {
		t.Errorf("stdout = %s, want the dryRun query param kept on the request", stdout)
	}
	_, _, err, sent = runDryRun(t, "list", "--dry-run", "true")
	if err != nil || !sent {
		t.Errorf("--dry-run should be the endpoint's own param here: err=%v sent=%v", err, sent)
	}
}

func TestDryRun_CompactOutput(t *testing.T) {
	exec := func(APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(dryRunSpec), exec)
	if err != nil {
		t.Fatal(err)
	}
	group := cmds[0]
	group.PersistentFlags().Bool("compact", false, "")
	var out bytes.Buffer
	group.SetOut(&out)
	group.SetArgs([]string{"create", "--dry-run", "--compact", "--body", `{"name":"x"}`})
	if err := group.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `{"valid":true,"method":"POST","path":"/things","bodyChecked":true}` {
		t.Errorf("compact output = %s", got)
	}
}
