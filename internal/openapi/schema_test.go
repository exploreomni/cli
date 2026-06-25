package openapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// schemaTestSpec exercises the body-schema describer: allOf composition,
// required-field union, field-level examples, enums, scalar placeholders,
// nested objects, arrays, and a self-referential ($ref-recursive) schema.
const schemaTestSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/widgets": {
      "post": {
        "operationId": "widgetsCreate",
        "tags": ["widgets"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/CreateWidget"}
            }
          }
        },
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "schemas": {
      "Base": {
        "type": "object",
        "required": ["baseField"],
        "properties": {
          "baseField": {"type": "string", "description": "from base", "example": "base-ex"}
        }
      },
      "Node": {
        "type": "object",
        "required": ["label"],
        "properties": {
          "label": {"type": "string"},
          "next": {"$ref": "#/components/schemas/Node"}
        }
      },
      "CreateWidget": {
        "allOf": [
          {"$ref": "#/components/schemas/Base"},
          {
            "type": "object",
            "required": ["name", "status", "count"],
            "properties": {
              "name": {"type": "string", "example": "my-widget"},
              "status": {"type": "string", "enum": ["active", "archived"]},
              "count": {"type": "integer"},
              "tags": {"type": "array", "items": {"type": "string"}},
              "child": {"$ref": "#/components/schemas/Node"}
            }
          }
        ]
      }
    }
  }
}`

// runSchema generates commands from a spec and runs the "widgets create"
// command with --schema, returning the parsed JSON document. It dispatches
// through the parent group with the full arg path, since cobra's Execute()
// re-parses from the root regardless of the receiver.
func runSchema(t *testing.T) bodySchemaDoc {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(schemaTestSpec), noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 group, got %d", len(cmds))
	}
	group := cmds[0]

	var buf bytes.Buffer
	group.SetOut(&buf)
	group.SetErr(&buf)
	group.SetArgs([]string{"create", "--schema"})
	if err := group.Execute(); err != nil {
		t.Fatalf("Execute --schema: %v\n%s", err, buf.String())
	}

	var doc bodySchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	return doc
}

func TestSchema_MetaAndRequiredUnion(t *testing.T) {
	doc := runSchema(t)
	if doc.Method != "POST" || doc.Path != "/api/v1/widgets" {
		t.Errorf("method/path = %q %q", doc.Method, doc.Path)
	}
	// Required is the union of the allOf members: baseField (Base) + name,
	// status, count (inline member).
	want := map[string]bool{"baseField": true, "name": true, "status": true, "count": true}
	if len(doc.Required) != len(want) {
		t.Errorf("required = %v, want %d entries", doc.Required, len(want))
	}
	for _, r := range doc.Required {
		if !want[r] {
			t.Errorf("unexpected required field %q", r)
		}
	}
}

func TestSchema_BodyMergesAllOfProperties(t *testing.T) {
	doc := runSchema(t)
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("body.properties missing")
	}
	// baseField comes from the allOf $ref; name/status/count from the inline
	// member — all flattened into one property set.
	for _, key := range []string{"baseField", "name", "status", "count", "tags", "child"} {
		if _, ok := props[key]; !ok {
			t.Errorf("merged properties missing %q", key)
		}
	}
}

func TestSchema_ExampleUsesExamplesEnumsAndPlaceholders(t *testing.T) {
	doc := runSchema(t)
	ex, ok := doc.Example.(map[string]interface{})
	if !ok {
		t.Fatalf("example is not an object: %T", doc.Example)
	}
	// Only required fields appear in the example skeleton.
	if _, ok := ex["tags"]; ok {
		t.Errorf("optional field tags should not be in example: %v", ex)
	}
	if _, ok := ex["child"]; ok {
		t.Errorf("optional field child should not be in example: %v", ex)
	}
	// Explicit field examples win.
	if ex["baseField"] != "base-ex" {
		t.Errorf("baseField = %v, want base-ex", ex["baseField"])
	}
	if ex["name"] != "my-widget" {
		t.Errorf("name = %v, want my-widget", ex["name"])
	}
	// Enum with no example uses the first enum value.
	if ex["status"] != "active" {
		t.Errorf("status = %v, want active (first enum)", ex["status"])
	}
	// Integer with no example gets a typed zero placeholder.
	if ex["count"] != float64(0) {
		t.Errorf("count = %v (%T), want 0", ex["count"], ex["count"])
	}
}

func TestSchema_RecursiveRefIsGuarded(t *testing.T) {
	doc := runSchema(t)
	// The Node schema references itself via "next"; the body dump must not loop
	// forever and must mark the recursion. Easiest check: the serialized body
	// contains the recursion note and is bounded in size.
	raw, _ := json.Marshal(doc.Body)
	if !strings.Contains(string(raw), "recursive reference") {
		t.Errorf("expected a recursion note in body for self-referential Node schema")
	}
	if len(raw) > 200_000 {
		t.Errorf("body unexpectedly large (%d bytes) — recursion may not be bounded", len(raw))
	}
}

// Without --schema, the command must still enforce normal behavior (here, that
// the body flag path is taken and the executor is invoked).
func TestSchema_FlagDoesNotAffectNormalRun(t *testing.T) {
	var called bool
	exec := func(req APIRequest) error { called = true; return nil }
	cmds, err := GenerateCommands([]byte(schemaTestSpec), exec)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	group := cmds[0]
	group.SilenceUsage = true
	group.SilenceErrors = true
	group.SetArgs([]string{"create", "--body", `{"name":"x"}`})
	if err := group.Execute(); err != nil {
		t.Fatalf("Execute with --body: %v", err)
	}
	if !called {
		t.Error("executor was not called on a normal (non --schema) run")
	}
}
