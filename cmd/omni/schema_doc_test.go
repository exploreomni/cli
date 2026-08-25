package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
)

// schemaDocFrom runs args plus --schema against cmd and returns the parsed
// document. stdout and stderr are captured separately so a test can assert that
// nothing but JSON reached stdout.
func schemaDocFrom(t *testing.T, cmd *cobra.Command, args ...string) (openapi.SchemaDoc, string) {
	t.Helper()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append(args, "--schema"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute %v --schema: %v\n%s%s", args, err, out.String(), errBuf.String())
	}
	var doc openapi.SchemaDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, out.String())
	}
	return doc, errBuf.String()
}

// specRoot builds a root command carrying the commands generated from the
// embedded spec, so a test can ask the real generator what an operation looks
// like.
func specRoot(t *testing.T) *cobra.Command {
	t.Helper()
	specData, err := specFS.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("reading embedded spec: %v", err)
	}
	cmds, err := openapi.GenerateCommands(specData, func(req openapi.APIRequest) error { return nil })
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	root := &cobra.Command{Use: "omni"}
	root.PersistentFlags().Bool("compact", false, "")
	for _, cmd := range cmds {
		root.AddCommand(cmd)
	}
	return root
}

// jsonOf renders a value the way --schema emits it, for order-insensitive
// comparison of two schema fragments.
func jsonOf(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// The hand-written commands' static documents are transcribed by hand, so they
// can drift from the spec silently. Each one wraps a generated operation; assert
// its response section is exactly what the describer produces for that
// operation, so a spec change that alters the response fails here.
func TestStaticSchemaDocs_ResponseMatchesSpec(t *testing.T) {
	cases := []struct {
		name      string
		static    openapi.SchemaDoc
		generated []string
	}{
		{"create-branch", createBranchSchemaDoc(), []string{"models", "create"}},
		{"set-attributes", setAttributesSchemaDoc(), []string{"scim", "users-update"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, _ := schemaDocFrom(t, specRoot(t), tc.generated...)
			if tc.static.Method != doc.Method || tc.static.Path != doc.Path {
				t.Fatalf("static doc describes %s %s, generated %s describes %s %s",
					tc.static.Method, tc.static.Path, strings.Join(tc.generated, " "), doc.Method, doc.Path)
			}
			if got, want := jsonOf(t, tc.static.Response), jsonOf(t, doc.Response); got != want {
				t.Errorf("static response has drifted from the spec\n static: %s\n   spec: %s", got, want)
			}
		})
	}
}

// The static body section is a hand-written description of the body the command
// actually assembles. Guard the field names against the assembly code so a
// change to one without the other is caught.
func TestCreateBranchSchemaDoc_BodyMatchesAssembledBody(t *testing.T) {
	assembled := buildCreateBranchBody("model-1", "conn-1", "branch")
	documented := createBranchSchemaDoc().Body.(map[string]interface{})["properties"].(map[string]interface{})

	for key := range assembled {
		if _, ok := documented[key]; !ok {
			t.Errorf("body field %q is assembled but not documented in --schema", key)
		}
	}
	for key := range documented {
		if _, ok := assembled[key]; !ok {
			t.Errorf("body field %q is documented in --schema but never assembled", key)
		}
	}
	// modelName is the only optional field: it is omitted when --name is unset.
	withoutName := buildCreateBranchBody("model-1", "conn-1", "")
	if _, ok := withoutName["modelName"]; ok {
		t.Error("modelName is sent with an empty --name; --schema documents it as omitted")
	}
	for _, key := range createBranchSchemaDoc().Required {
		if _, ok := withoutName[key]; !ok {
			t.Errorf("required field %q is missing from the assembled body", key)
		}
	}
}

// Same drift guard for set-attributes: its documented SCIM PatchOp fields must
// be the ones buildSetAttributesBody actually emits.
func TestSetAttributesSchemaDoc_BodyMatchesAssembledBody(t *testing.T) {
	raw, err := buildSetAttributesBody([]string{"region=us-east"}, "")
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	var assembled map[string]interface{}
	if err := json.Unmarshal(raw, &assembled); err != nil {
		t.Fatalf("unmarshal assembled body: %v", err)
	}

	documented := setAttributesSchemaDoc().Body.(map[string]interface{})["properties"].(map[string]interface{})
	for key := range assembled {
		if _, ok := documented[key]; !ok {
			t.Errorf("body field %q is assembled but not documented in --schema", key)
		}
	}
	for key := range documented {
		if _, ok := assembled[key]; !ok {
			t.Errorf("body field %q is documented in --schema but never assembled", key)
		}
	}

	// The operation object's own fields, one level in.
	ops, ok := assembled["Operations"].([]interface{})
	if !ok || len(ops) != 1 {
		t.Fatalf("Operations = %v, want exactly one op", assembled["Operations"])
	}
	op := ops[0].(map[string]interface{})
	opProps := documented["Operations"].(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
	for key := range op {
		if _, ok := opProps[key]; !ok {
			t.Errorf("Operations item field %q is assembled but not documented", key)
		}
	}
	if op["op"] != "replace" {
		t.Errorf("op = %v, want replace as documented", op["op"])
	}
	if _, ok := op["value"].(map[string]interface{})[userAttributePrefix]; !ok {
		t.Errorf("value is not keyed by %s as documented: %v", userAttributePrefix, op["value"])
	}
}

// --depth must truncate a hand-written command's static document exactly as it
// truncates a generated one, rather than being accepted and ignored.
func TestStaticSchemaDocs_DepthTruncates(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func() *cobra.Command
		args   []string
		// a body property that is a nested object at depth 1
		nested string
	}{
		{
			name:   "create-branch",
			newCmd: func() *cobra.Command { return createBranchCmd(func(req openapi.APIRequest) error { return nil }) },
			nested: "modelKind",
		},
		{
			name:   "set-attributes",
			newCmd: func() *cobra.Command { return setUserAttributesCmd(func(req openapi.APIRequest) error { return nil }) },
			nested: "Operations",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full, _ := schemaDocFrom(t, tc.newCmd())
			shallow, _ := schemaDocFrom(t, tc.newCmd(), "--depth", "0")
			if jsonOf(t, full) == jsonOf(t, shallow) {
				t.Fatal("--depth 0 produced the default document; the flag is a no-op")
			}

			props := shallow.Body.(map[string]interface{})["properties"].(map[string]interface{})
			node, ok := props[tc.nested].(map[string]interface{})
			if !ok {
				t.Fatalf("body.properties.%s missing: %v", tc.nested, props)
			}
			if node["note"] != "max depth reached; expansion omitted" {
				t.Errorf("body.properties.%s = %v, want the depth placeholder", tc.nested, node)
			}
			// The placeholder keeps the node's type, as the describer's does.
			if _, ok := node["type"]; !ok {
				t.Errorf("depth placeholder dropped the type: %v", node)
			}
			// The response section honors --depth too.
			respProps := shallow.Response.Schema.(map[string]interface{})["properties"].(map[string]interface{})
			for name, v := range respProps {
				if n, ok := v.(map[string]interface{}); !ok || n["note"] != "max depth reached; expansion omitted" {
					t.Errorf("response property %q was not truncated: %v", name, v)
				}
			}

			// A deeper budget expands more than a shallower one.
			deeper, _ := schemaDocFrom(t, tc.newCmd(), "--depth", "1")
			if jsonOf(t, deeper) == jsonOf(t, shallow) {
				t.Error("--depth 1 matched --depth 0; truncation is not depth-sensitive")
			}
		})
	}
}

// A deprecated operation's --schema output must be pure JSON on stdout: cobra
// would otherwise print its deprecation notice ahead of the document. PUT
// /api/v1/documents/{identifier} is deprecated in the shipped spec.
func TestDeprecatedCommand_SchemaEmitsCleanJSON(t *testing.T) {
	root := specRoot(t)
	var out, errBuf bytes.Buffer
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"documents", "put", "--schema", "--compact"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute --schema: %v\n%s%s", err, out.String(), errBuf.String())
	}
	var doc openapi.SchemaDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not pure JSON (deprecation notice leaked?): %v\n%s", err, out.String())
	}
	if doc.Method != "PUT" {
		t.Errorf("method = %q, want PUT", doc.Method)
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr = %q, want nothing on a --schema run", errBuf.String())
	}
}
