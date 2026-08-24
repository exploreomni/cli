package openapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
func runSchema(t *testing.T) SchemaDoc {
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

	var doc SchemaDoc
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

// mapTestSpec exercises a map-typed (additionalProperties) field — the shape of
// documents v2-create's queryPresentations.data, which keys tile objects by tab
// ID. The value schema lives in additionalProperties, not properties.
const mapTestSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/docs": {
      "post": {
        "operationId": "docsCreate",
        "tags": ["docs"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/CreateDoc"}
            }
          }
        },
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "schemas": {
      "Tile": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name": {"type": "string", "example": "Revenue"},
          "prefersChart": {"type": "boolean"}
        }
      },
      "CreateDoc": {
        "type": "object",
        "required": ["presentations"],
        "properties": {
          "presentations": {
            "type": "object",
            "description": "Tiles keyed by tab ID.",
            "additionalProperties": {"$ref": "#/components/schemas/Tile"}
          }
        }
      }
    }
  }
}`

func runMapSchema(t *testing.T) SchemaDoc {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(mapTestSpec), noop)
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
	var doc SchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	return doc
}

// A map-typed field must expand its value schema from additionalProperties
// rather than dropping it (the queryPresentations.data gap).
func TestSchema_MapExpandsAdditionalProperties(t *testing.T) {
	doc := runMapSchema(t)
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("body.properties missing")
	}
	pres, ok := props["presentations"].(map[string]interface{})
	if !ok {
		t.Fatalf("presentations property missing or not an object")
	}
	addl, ok := pres["additionalProperties"].(map[string]interface{})
	if !ok {
		t.Fatalf("presentations.additionalProperties missing — map value schema was dropped")
	}
	tileProps, ok := addl["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("additionalProperties.properties missing — Tile $ref not expanded")
	}
	for _, key := range []string{"name", "prefersChart"} {
		if _, ok := tileProps[key]; !ok {
			t.Errorf("expanded tile schema missing %q", key)
		}
	}
}

// The synthesized example must render one representative map entry so the value
// shape is copy-pasteable, not an empty object.
func TestSchema_MapExampleShowsRepresentativeEntry(t *testing.T) {
	doc := runMapSchema(t)
	ex, ok := doc.Example.(map[string]interface{})
	if !ok {
		t.Fatalf("example is not an object: %T", doc.Example)
	}
	pres, ok := ex["presentations"].(map[string]interface{})
	if !ok {
		t.Fatalf("example.presentations missing or not an object: %v", ex)
	}
	entry, ok := pres["<key>"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a <key> sample entry in the map example, got %v", pres)
	}
	if entry["name"] != "Revenue" {
		t.Errorf("sample tile name = %v, want Revenue (field example)", entry["name"])
	}
}

// describeRefTestSpec exercises the `.describe()`-on-a-ref pattern: a typed ref
// wrapped in an allOf alongside a description-only sibling. `containers` wraps an
// array ref (Layout) and `identifier` wraps a scalar ref (DocId). A naive
// allOf merge that only harvests object properties/required drops these to an
// empty object; the describer must instead adopt the underlying shape.
const describeRefTestSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/docs": {
      "post": {
        "operationId": "docsCreate",
        "tags": ["docs"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/CreateDoc"}
            }
          }
        },
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "schemas": {
      "Grid": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string", "example": "grid"}}},
      "Stack": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string", "example": "stack"}}},
      "Layout": {
        "type": "array",
        "description": "Container layout array.",
        "items": {"anyOf": [{"$ref": "#/components/schemas/Grid"}, {"$ref": "#/components/schemas/Stack"}]}
      },
      "DocId": {"type": "string", "minLength": 2, "description": "Base identifier description."},
      "CreateDoc": {
        "type": "object",
        "required": ["containers", "identifier"],
        "properties": {
          "containers": {
            "allOf": [
              {"$ref": "#/components/schemas/Layout"},
              {"description": "Override: when present, replaces the existing layout."}
            ]
          },
          "identifier": {
            "allOf": [
              {"$ref": "#/components/schemas/DocId"},
              {"description": "Override: identifier for the new document."}
            ]
          }
        }
      }
    }
  }
}`

// An array ref wrapped in a describe()-allOf must keep its array shape and items
// union, not collapse to an empty object.
func TestSchema_DescribeOnRefArrayKeepsShape(t *testing.T) {
	doc, err := runSchemaWith(t, describeRefTestSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := doc.Body.(map[string]interface{})
	props := body["properties"].(map[string]interface{})
	containers, ok := props["containers"].(map[string]interface{})
	if !ok {
		t.Fatalf("containers property missing or not an object")
	}
	if containers["type"] != "array" {
		t.Errorf("containers.type = %v, want array (shape lost to allOf merge)", containers["type"])
	}
	items, ok := containers["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("containers.items missing — array shape was dropped: %v", containers)
	}
	anyOf, ok := items["anyOf"].([]interface{})
	if !ok || len(anyOf) != 2 {
		t.Errorf("containers.items.anyOf = %v, want 2 members (Grid/Stack)", items["anyOf"])
	}
	// The description-only sibling overrides the ref's own description.
	if got, _ := containers["description"].(string); !strings.HasPrefix(got, "Override:") {
		t.Errorf("containers.description = %q, want the sibling override to win", got)
	}
}

// A scalar ref wrapped in a describe()-allOf must keep its scalar type and take
// the sibling's overriding description.
func TestSchema_DescribeOnRefScalarKeepsType(t *testing.T) {
	doc, err := runSchemaWith(t, describeRefTestSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := doc.Body.(map[string]interface{})
	props := body["properties"].(map[string]interface{})
	identifier, ok := props["identifier"].(map[string]interface{})
	if !ok {
		t.Fatalf("identifier property missing or not an object")
	}
	if identifier["type"] != "string" {
		t.Errorf("identifier.type = %v, want string (scalar shape lost)", identifier["type"])
	}
	if got, _ := identifier["description"].(string); !strings.HasPrefix(got, "Override:") {
		t.Errorf("identifier.description = %q, want the sibling override to win", got)
	}
}

// The synthesized example must reflect the underlying typed member: an array for
// the array ref and a scalar placeholder for the scalar ref, not empty objects.
func TestSchema_DescribeOnRefExampleSynthesizesShape(t *testing.T) {
	doc, err := runSchemaWith(t, describeRefTestSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ex, ok := doc.Example.(map[string]interface{})
	if !ok {
		t.Fatalf("example is not an object: %T", doc.Example)
	}
	arr, ok := ex["containers"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Errorf("example.containers = %v, want a one-element array (got empty object?)", ex["containers"])
	}
	if s, ok := ex["identifier"].(string); !ok || !strings.HasPrefix(s, "<") {
		t.Errorf("example.identifier = %v, want a scalar placeholder", ex["identifier"])
	}
}

// Drilling --field into a describe()-on-a-ref array resolves and expands it,
// rather than returning an empty body.
func TestSchema_DescribeOnRefFieldDrill(t *testing.T) {
	doc, err := runSchemaWith(t, describeRefTestSpec, "--field", "containers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	if body["type"] != "array" {
		t.Errorf("drilled containers.type = %v, want array", body["type"])
	}
	if _, ok := body["items"].(map[string]interface{}); !ok {
		t.Errorf("drilled containers.items missing: %v", body)
	}
}

// runSchemaWith runs "create --schema" plus extra flags against a spec. On
// success it returns the parsed doc; on failure it returns the execution error
// (usage/errors silenced so the error carries only the message under test).
func runSchemaWith(t *testing.T, spec string, extra ...string) (SchemaDoc, error) {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(spec), noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	group := cmds[0]
	group.SilenceUsage = true
	group.SilenceErrors = true
	var buf bytes.Buffer
	group.SetOut(&buf)
	group.SetErr(&buf)
	group.SetArgs(append([]string{"create", "--schema"}, extra...))
	if execErr := group.Execute(); execErr != nil {
		return SchemaDoc{}, execErr
	}
	var doc SchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	return doc, nil
}

// --field drills to a nested property; the $ref ("child" → Node) is resolved
// and expanded so the drilled body is the Node object, not the whole widget.
func TestSchema_FieldDrillsNestedProperty(t *testing.T) {
	doc, err := runSchemaWith(t, schemaTestSpec, "--field", "child")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Field != "child" {
		t.Errorf("field = %q, want child", doc.Field)
	}
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("drilled body.properties missing")
	}
	if _, ok := props["label"]; !ok {
		t.Errorf("expected Node.label in drilled body, got %v", props)
	}
}

// A dotted path transparently descends through a map (additionalProperties):
// "presentations.prefersChart" reaches the Tile's boolean leaf without the
// caller naming the map's value layer.
func TestSchema_FieldAutoDescendsMap(t *testing.T) {
	doc, err := runSchemaWith(t, mapTestSpec, "--field", "presentations.prefersChart")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	if body["type"] != "boolean" {
		t.Errorf("drilled leaf type = %v, want boolean", body["type"])
	}
}

// An unknown field errors and lists the fields available at that level — which,
// after descending through the map, are the Tile's fields.
func TestSchema_FieldNotFoundListsAvailable(t *testing.T) {
	_, err := runSchemaWith(t, mapTestSpec, "--field", "presentations.bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	msg := err.Error()
	if !strings.Contains(msg, `no field "bogus"`) {
		t.Errorf("error = %q, want it to name the missing field", msg)
	}
	for _, want := range []string{"name", "prefersChart"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to list available field %q", msg, want)
		}
	}
}

// --depth bounds expansion: at depth 0 the top-level object lists its
// properties but nested objects are truncated with the depth note.
func TestSchema_DepthLimitsExpansion(t *testing.T) {
	doc, err := runSchemaWith(t, schemaTestSpec, "--depth", "0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(doc.Body)
	if !strings.Contains(string(raw), "max depth reached") {
		t.Errorf("expected a depth-truncation note at --depth 0: %s", raw)
	}
	// The deep default must still fully expand (no truncation note).
	full, _ := runSchemaWith(t, schemaTestSpec)
	rawFull, _ := json.Marshal(full.Body)
	if strings.Contains(string(rawFull), "max depth reached") {
		t.Errorf("default depth should not truncate this small schema: %s", rawFull)
	}
}

// bodylessTestSpec exercises --schema on operations that take no request body:
// a GET with a path param, two query params (one enum-valued and required) and a
// bare-array 200 response, plus a DELETE whose only success status (204) carries
// no content. The 202 is declared before the 200 so the "lowest 2xx wins"
// selection can't pass by accident of declaration order, and text/csv sits
// before application/json so the media-type preference is exercised too.
const bodylessTestSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/widgets/{widgetId}/checks": {
      "get": {
        "operationId": "widgetsChecks",
        "tags": ["widgets"],
        "parameters": [
          {"name": "widgetId", "in": "path", "required": true, "description": "Widget UUID", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "description": "Max results", "schema": {"type": "integer"}},
          {"name": "severity", "in": "query", "required": true, "description": "Filter by severity", "schema": {"type": "string", "enum": ["error", "warning"]}}
        ],
        "responses": {
          "202": {"description": "queued", "content": {"application/json": {"schema": {"type": "object"}}}},
          "200": {
            "description": "Check results",
            "content": {
              "text/csv": {"schema": {"type": "string"}},
              "application/json": {
                "schema": {
                  "type": "array",
                  "description": "Bare top-level array of issues.",
                  "items": {
                    "type": "object",
                    "required": ["message"],
                    "properties": {"message": {"type": "string"}, "nested": {"type": "object", "properties": {"deep": {"type": "string"}}}}
                  }
                }
              }
            }
          },
          "404": {"description": "not found"}
        }
      },
      "delete": {
        "operationId": "widgetsDeleteChecks",
        "tags": ["widgets"],
        "parameters": [
          {"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {"204": {"description": "Checks cleared"}}
      }
    }
  }
}`

// runSchemaCmd runs "<name> --schema" plus extra flags against a spec, with no
// positional args — --schema must short-circuit before arg validation.
func runSchemaCmd(t *testing.T, spec, name string, extra ...string) (SchemaDoc, error) {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(spec), noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	group := cmds[0]
	group.SilenceUsage = true
	group.SilenceErrors = true
	var buf bytes.Buffer
	group.SetOut(&buf)
	group.SetErr(&buf)
	group.SetArgs(append([]string{name, "--schema"}, extra...))
	if execErr := group.Execute(); execErr != nil {
		return SchemaDoc{}, execErr
	}
	var doc SchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	return doc, nil
}

// --schema on a bodyless GET works with no positional args and describes the
// call: args, query flags, and an explicit null body.
func TestSchema_BodylessGetDescribesArgsAndQueryParams(t *testing.T) {
	doc, err := runSchemaCmd(t, bodylessTestSpec, "checks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Method != "GET" || doc.Path != "/api/v1/widgets/{widgetId}/checks" {
		t.Errorf("method/path = %q %q", doc.Method, doc.Path)
	}
	if doc.Body != nil {
		t.Errorf("body = %v, want null for a bodyless operation", doc.Body)
	}
	if len(doc.Args) != 1 {
		t.Fatalf("args = %v, want 1 entry", doc.Args)
	}
	arg := doc.Args[0]
	if arg.Name != "widgetId" || arg.Placeholder != "<widgetid>" || arg.Type != "string" || arg.Description != "Widget UUID" {
		t.Errorf("arg = %+v, want the widgetId path param", arg)
	}

	byFlag := map[string]SchemaQueryParam{}
	for _, q := range doc.QueryParams {
		byFlag[q.Flag] = q
	}
	limit, ok := byFlag["--limit"]
	if !ok {
		t.Fatalf("query params %v missing --limit", doc.QueryParams)
	}
	if limit.Type != "integer" || limit.Required || limit.Description != "Max results" {
		t.Errorf("--limit = %+v, want an optional integer with its description", limit)
	}
	severity, ok := byFlag["--severity"]
	if !ok {
		t.Fatalf("query params %v missing --severity", doc.QueryParams)
	}
	if !severity.Required {
		t.Errorf("--severity.required = false, want true")
	}
	if strings.Join(severity.Enum, ",") != "error,warning" {
		t.Errorf("--severity.enum = %v, want [error warning]", severity.Enum)
	}
}

// The response section reports the lowest declared 2xx, prefers application/json
// over other media types, and renders a bare top-level array as such — the shape
// a caller would otherwise have to discover by crashing on it.
func TestSchema_ResponseBareArray(t *testing.T) {
	doc, err := runSchemaCmd(t, bodylessTestSpec, "checks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := doc.Response
	if resp == nil {
		t.Fatal("response section missing")
	}
	if resp.Status != "200" {
		t.Errorf("response.status = %q, want 200 (lowest 2xx wins over 202)", resp.Status)
	}
	if resp.ContentType != "application/json" {
		t.Errorf("response.contentType = %q, want application/json", resp.ContentType)
	}
	if resp.Description != "Check results" {
		t.Errorf("response.description = %q", resp.Description)
	}
	schema, ok := resp.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("response.schema is not an object: %T", resp.Schema)
	}
	if schema["type"] != "array" {
		t.Errorf("response.schema.type = %v, want array", schema["type"])
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("response.schema.items missing: %v", schema)
	}
	props, ok := items["properties"].(map[string]interface{})
	if !ok || props["message"] == nil {
		t.Errorf("response item properties = %v, want a message field", items["properties"])
	}
}

// A 2xx with no content still reports the status, so a caller can tell "no body"
// from "unknown".
func TestSchema_ResponseWithoutContent(t *testing.T) {
	doc, err := runSchemaCmd(t, bodylessTestSpec, "delete-checks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Response == nil {
		t.Fatal("response section missing")
	}
	if doc.Response.Status != "204" {
		t.Errorf("response.status = %q, want 204", doc.Response.Status)
	}
	if doc.Response.Schema != nil {
		t.Errorf("response.schema = %v, want null for a contentless status", doc.Response.Schema)
	}
}

// --depth bounds the response expansion too, not just the request body.
func TestSchema_DepthLimitsResponseExpansion(t *testing.T) {
	doc, err := runSchemaCmd(t, bodylessTestSpec, "checks", "--depth", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(doc.Response)
	if !strings.Contains(string(raw), "max depth reached") {
		t.Errorf("expected a depth-truncation note in the response at --depth 1: %s", raw)
	}
}

// --field only drills the request body, so asking for it on a bodyless operation
// must say so plainly rather than silently returning an empty body.
func TestSchema_FieldOnBodylessOperationErrors(t *testing.T) {
	_, err := runSchemaCmd(t, bodylessTestSpec, "checks", "--field", "anything")
	if err == nil {
		t.Fatal("expected an error for --field on a bodyless operation")
	}
	if !strings.Contains(err.Error(), "request body") {
		t.Errorf("error = %q, want it to explain that --field needs a request body", err.Error())
	}
}

// A body operation's document keeps its established shape (method, path,
// required, body, example) and gains the response section — existing consumers
// must not break.
func TestSchema_BodyOperationStaysBackwardCompatible(t *testing.T) {
	doc := runSchema(t)
	if doc.Method != "POST" || doc.Path != "/api/v1/widgets" {
		t.Errorf("method/path = %q %q", doc.Method, doc.Path)
	}
	if len(doc.Required) == 0 {
		t.Error("required is empty; the body op's required list regressed")
	}
	if _, ok := doc.Body.(map[string]interface{}); !ok {
		t.Errorf("body is not an object: %T", doc.Body)
	}
	if _, ok := doc.Example.(map[string]interface{}); !ok {
		t.Errorf("example is not an object: %T", doc.Example)
	}
	// schemaTestSpec's 200 declares a description but no content.
	if doc.Response == nil || doc.Response.Status != "200" {
		t.Errorf("response = %+v, want the declared 200", doc.Response)
	}
}

// collidingParamsSpec declares query params literally named schema, field and
// depth — all three preferred discovery flag names. The params must keep their
// own flags (they are the endpoint's contract) and discovery must still be
// reachable, under its fallback names.
const collidingParamsSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/things": {
      "get": {
        "operationId": "thingsList",
        "tags": ["things"],
        "parameters": [
          {"name": "schema", "in": "query", "description": "Database schema to inspect", "schema": {"type": "string"}},
          {"name": "field", "in": "query", "description": "Field to group by", "schema": {"type": "string"}},
          {"name": "depth", "in": "query", "description": "Traversal depth", "schema": {"type": "integer"}}
        ],
        "responses": {
          "200": {
            "description": "Things",
            "content": {"application/json": {"schema": {"type": "object", "properties": {"a": {"type": "object", "properties": {"b": {"type": "string"}}}}}}}
          }
        }
      }
    }
  }
}`

// A spec param owning a discovery flag name must not disable discovery: the
// flags move to deterministic fallbacks, and the document reports where they
// went.
func TestSchema_ParamNameCollisionFallsBackToPrefixedFlags(t *testing.T) {
	doc, err := runSchemaCmdFlag(t, collidingParamsSpec, "list", "schema-doc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Method != "GET" || doc.Path != "/api/v1/things" {
		t.Errorf("method/path = %q %q", doc.Method, doc.Path)
	}
	if doc.SchemaFlags == nil {
		t.Fatal("schemaFlags section missing; agents can't discover the renamed flags")
	}
	want := SchemaFlags{Schema: "schema-doc", Field: "schema-field", Depth: "schema-depth"}
	if *doc.SchemaFlags != want {
		t.Errorf("schemaFlags = %+v, want %+v", *doc.SchemaFlags, want)
	}
	// The colliding query params are still described as query params.
	flags := map[string]bool{}
	for _, q := range doc.QueryParams {
		flags[q.Flag] = true
	}
	for _, f := range []string{"--schema", "--field", "--depth"} {
		if !flags[f] {
			t.Errorf("query param %s missing from the document", f)
		}
	}
}

// The renamed refinement flags must work, and the renaming must be visible in
// --help so a reader can find them.
func TestSchema_RenamedRefinementFlagsWork(t *testing.T) {
	doc, err := runSchemaCmdFlag(t, collidingParamsSpec, "list", "schema-doc", "--schema-depth", "0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(doc.Response)
	if !strings.Contains(string(raw), "max depth reached") {
		t.Errorf("--schema-depth 0 did not bound the response: %s", raw)
	}

	sub := subcommand(t, collidingParamsSpec, "list")
	help := sub.LocalFlags().FlagUsages()
	for _, want := range []string{"--schema-doc", "--schema-field", "--schema-depth", "because --schema is a parameter"} {
		if !strings.Contains(help, want) {
			t.Errorf("flag help does not mention %q:\n%s", want, help)
		}
	}
}

// A colliding param must still be sent as a query param — never swallowed as a
// discovery control.
func TestSchema_CollidingParamsStillReachTheRequest(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }
	cmds, err := GenerateCommands([]byte(collidingParamsSpec), exec)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	group := cmds[0]
	group.SilenceUsage = true
	group.SilenceErrors = true
	group.SetArgs([]string{"list", "--schema", "public", "--field", "name", "--depth", "3"})
	if err := group.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"schema=public", "field=name", "depth=3"} {
		if !strings.Contains(captured.Path, want) {
			t.Errorf("path %q missing %s", captured.Path, want)
		}
	}
}

// wildcardResponseSpec exercises response declarations that are easy to lose:
// an operation whose only success status is the "2XX" range key, one where a
// specific code must beat the range key, and one whose media type declares no
// schema.
const wildcardResponseSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1.0"},
  "paths": {
    "/api/v1/ranged": {
      "get": {
        "operationId": "rangedOnly",
        "tags": ["resp"],
        "responses": {
          "2XX": {"description": "Ranged success", "content": {"application/json": {"schema": {"type": "object", "properties": {"ok": {"type": "boolean"}}}}}},
          "4XX": {"description": "Ranged failure"}
        }
      }
    },
    "/api/v1/both": {
      "get": {
        "operationId": "rangedAndSpecific",
        "tags": ["resp"],
        "responses": {
          "2XX": {"description": "Ranged success", "content": {"application/json": {"schema": {"type": "string"}}}},
          "201": {"description": "Created", "content": {"application/json": {"schema": {"type": "object", "properties": {"id": {"type": "string"}}}}}}
        }
      }
    },
    "/api/v1/schemaless": {
      "get": {
        "operationId": "schemalessMedia",
        "tags": ["resp"],
        "responses": {
          "200": {"description": "A file", "content": {"text/csv": {}}}
        }
      }
    },
    "/api/v1/mixed": {
      "get": {
        "operationId": "mixedMedia",
        "tags": ["resp"],
        "responses": {
          "200": {
            "description": "Mixed",
            "content": {"application/json": {}, "text/ndjson": {"schema": {"type": "object", "properties": {"row": {"type": "string"}}}}}
          }
        }
      }
    }
  }
}`

// A success response declared only under the "2XX" range key must still be
// reported rather than dropped.
func TestSchema_ResponseAcceptsRangeWildcard(t *testing.T) {
	doc, err := runSchemaCmd(t, wildcardResponseSpec, "ranged-only")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Response == nil {
		t.Fatal("response section missing for a 2XX-only operation")
	}
	if doc.Response.Status != "2XX" {
		t.Errorf("response.status = %q, want 2XX", doc.Response.Status)
	}
	schema, ok := doc.Response.Schema.(map[string]interface{})
	if !ok || schema["type"] != "object" {
		t.Errorf("response.schema = %v, want the declared object", doc.Response.Schema)
	}
}

// A specific 2xx code beats the range wildcard.
func TestSchema_SpecificCodeBeatsRangeWildcard(t *testing.T) {
	doc, err := runSchemaCmd(t, wildcardResponseSpec, "ranged-and-specific")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Response == nil || doc.Response.Status != "201" {
		t.Fatalf("response = %+v, want the specific 201 to win over 2XX", doc.Response)
	}
	schema, ok := doc.Response.Schema.(map[string]interface{})
	if !ok || schema["type"] != "object" {
		t.Errorf("response.schema = %v, want the 201 object, not the 2XX string", doc.Response.Schema)
	}
}

// A media type declared without a schema must still be reported by name —
// "text/csv, shape undocumented" beats implying an empty response.
func TestSchema_ResponseMediaTypeWithoutSchema(t *testing.T) {
	doc, err := runSchemaCmd(t, wildcardResponseSpec, "schemaless-media")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Response == nil {
		t.Fatal("response section missing")
	}
	if doc.Response.ContentType != "text/csv" {
		t.Errorf("response.contentType = %q, want text/csv (the declared media type was dropped)", doc.Response.ContentType)
	}
	if doc.Response.Schema != nil {
		t.Errorf("response.schema = %v, want null for an undocumented media type", doc.Response.Schema)
	}
}

// When application/json declares no schema but another media type does, report
// the one that actually carries a shape.
func TestSchema_ResponsePrefersMediaTypeThatHasASchema(t *testing.T) {
	doc, err := runSchemaCmd(t, wildcardResponseSpec, "mixed-media")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Response == nil {
		t.Fatal("response section missing")
	}
	if doc.Response.ContentType != "text/ndjson" {
		t.Errorf("response.contentType = %q, want text/ndjson (the only type with a schema)", doc.Response.ContentType)
	}
	if doc.Response.Schema == nil {
		t.Error("response.schema is null; the declared ndjson schema was dropped")
	}
}

// subcommand generates commands from a spec and returns the named subcommand of
// the first group.
func subcommand(t *testing.T, spec, name string) *cobra.Command {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(spec), noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

// runSchemaCmdFlag is runSchemaCmd for commands whose discovery flag was
// renamed by a parameter collision.
func runSchemaCmdFlag(t *testing.T, spec, name, schemaFlag string, extra ...string) (SchemaDoc, error) {
	t.Helper()
	noop := func(req APIRequest) error { return nil }
	cmds, err := GenerateCommands([]byte(spec), noop)
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	group := cmds[0]
	group.SilenceUsage = true
	group.SilenceErrors = true
	var buf bytes.Buffer
	group.SetOut(&buf)
	group.SetErr(&buf)
	group.SetArgs(append([]string{name, "--" + schemaFlag}, extra...))
	if execErr := group.Execute(); execErr != nil {
		return SchemaDoc{}, execErr
	}
	var doc SchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	return doc, nil
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
