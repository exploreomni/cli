package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
)

type parsedOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

type parsedPatchBody struct {
	Schemas    []string   `json:"schemas"`
	Operations []parsedOp `json:"Operations"`
}

func parseBody(t *testing.T, b []byte) parsedPatchBody {
	t.Helper()
	var body parsedPatchBody
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// patchedAttrs returns the namespaced attribute map from the single replace op,
// i.e. value["urn:omni:params:1.0:UserAttribute"].
func patchedAttrs(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	body := parseBody(t, b)
	if len(body.Schemas) != 1 || body.Schemas[0] != "urn:ietf:params:scim:api:messages:2.0:PatchOp" {
		t.Errorf("schemas = %v, want PatchOp", body.Schemas)
	}
	if len(body.Operations) != 1 {
		t.Fatalf("ops = %d, want 1 (no-path object form)", len(body.Operations))
	}
	op := body.Operations[0]
	if op.Op != "replace" || op.Path != "" {
		t.Errorf("op = {op:%q path:%q}, want replace with no path", op.Op, op.Path)
	}
	val, ok := op.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("value is %T, want object", op.Value)
	}
	attrs, ok := val["urn:omni:params:1.0:UserAttribute"].(map[string]interface{})
	if !ok {
		t.Fatalf("value missing urn:omni:params:1.0:UserAttribute object: %v", val)
	}
	return attrs
}

// TestBuildSetAttributesBody_StringAttrs verifies attributes are nested under
// the Omni namespace in a single no-path replace op.
func TestBuildSetAttributesBody_StringAttrs(t *testing.T) {
	b, err := buildSetAttributesBody([]string{"region=us-east", "team=growth"}, "")
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	attrs := patchedAttrs(t, b)
	if attrs["region"] != "us-east" {
		t.Errorf("region = %v, want us-east", attrs["region"])
	}
	if attrs["team"] != "growth" {
		t.Errorf("team = %v, want growth", attrs["team"])
	}
}

// TestBuildSetAttributesBody_ClearAttr verifies an empty value sets the
// attribute to an explicit null (present in the payload, not omitted).
func TestBuildSetAttributesBody_ClearAttr(t *testing.T) {
	b, err := buildSetAttributesBody([]string{"region="}, "")
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	attrs := patchedAttrs(t, b)
	if v, ok := attrs["region"]; !ok || v != nil {
		t.Errorf("region = %v (present=%v), want explicit null", v, ok)
	}
	if !strings.Contains(string(b), `"region":null`) {
		t.Errorf("body should contain explicit null value, got %s", b)
	}
}

// TestBuildSetAttributesBody_AttrJSON verifies --attr-json contributes typed
// (numeric/array) values.
func TestBuildSetAttributesBody_AttrJSON(t *testing.T) {
	b, err := buildSetAttributesBody(nil, `{"regions":["us","eu"],"level":3}`)
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	attrs := patchedAttrs(t, b)
	if v, ok := attrs["level"].(float64); !ok || v != 3 {
		t.Errorf("level = %v (%T), want number 3", attrs["level"], attrs["level"])
	}
	arr, ok := attrs["regions"].([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "us" || arr[1] != "eu" {
		t.Errorf("regions = %v, want [us eu]", attrs["regions"])
	}
}

// TestBuildSetAttributesBody_Errors covers the validation failure modes.
func TestBuildSetAttributesBody_Errors(t *testing.T) {
	cases := []struct {
		name     string
		attrs    []string
		attrJSON string
		wantSub  string
	}{
		{"no input", nil, "", "at least one"},
		{"missing equals", []string{"region"}, "", "key=value"},
		{"empty key", []string{"=us-east"}, "", "empty attribute name"},
		{"dup across attrs", []string{"region=a", "region=b"}, "", "more than once"},
		{"dup across attr and json", []string{"region=a"}, `{"region":"b"}`, "more than once"},
		{"invalid json", nil, `{not json}`, "invalid --attr-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildSetAttributesBody(tc.attrs, tc.attrJSON)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestSetUserAttributesCmd_RequestShape verifies the command issues a PATCH to
// the SCIM user path (with the id URL-escaped) carrying the assembled body.
func TestSetUserAttributesCmd_RequestShape(t *testing.T) {
	var captured openapi.APIRequest
	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error {
		captured = req
		return nil
	})
	cmd.SetArgs([]string{"abc/def", "--attr", "region=us-east"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if captured.Method != "PATCH" {
		t.Errorf("method = %q, want PATCH", captured.Method)
	}
	if captured.Path != "/api/scim/v2/Users/abc%2Fdef" {
		t.Errorf("path = %q, want id URL-escaped", captured.Path)
	}
	if attrs := patchedAttrs(t, captured.Body); attrs["region"] != "us-east" {
		t.Errorf("region = %v, want us-east", attrs["region"])
	}
}

func TestIsEmail(t *testing.T) {
	cases := map[string]bool{
		"user@example.com":                     true,
		"first.last+tag@sub.example.co":        true,
		"550e8400-e29b-41d4-a716-446655440000": false,
		"abc/def":                              false,
		"":                                     false,
	}
	for in, want := range cases {
		if got := isEmail(in); got != want {
			t.Errorf("isEmail(%q) = %v, want %v", in, got, want)
		}
	}
}

// scimListServer returns a test server that answers the SCIM Users list with
// the given resources and records the filter query it received.
func scimListServer(t *testing.T, resources []map[string]interface{}, gotFilter *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/scim/v2/Users" {
			if gotFilter != nil {
				*gotFilter = r.URL.Query().Get("filter")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"Resources": resources})
			return
		}
		http.Error(w, "unexpected request", 500)
	}))
}

func setEmailLookupEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("OMNI_API_TOKEN", "test-token")
	t.Setenv("OMNI_BASE_URL", baseURL)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")
}

// TestSetUserAttributesCmd_ResolvesEmail verifies that an email argument is
// resolved to the user's SCIM id (via a userName filter) before the PATCH, and
// that the match is case-insensitive.
func TestSetUserAttributesCmd_ResolvesEmail(t *testing.T) {
	var gotFilter string
	ts := scimListServer(t, []map[string]interface{}{
		{"id": "user-uuid-123", "userName": "User@Example.com"},
	}, &gotFilter)
	defer ts.Close()
	setEmailLookupEnv(t, ts.URL)

	var captured openapi.APIRequest
	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error {
		captured = req
		return nil
	})
	cmd.SetArgs([]string{"user@example.com", "--attr", "region=us-east"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotFilter != `userName eq "user@example.com"` {
		t.Errorf("filter = %q, want `userName eq \"user@example.com\"`", gotFilter)
	}
	if captured.Path != "/api/scim/v2/Users/user-uuid-123" {
		t.Errorf("path = %q, want /api/scim/v2/Users/user-uuid-123", captured.Path)
	}
}

// TestSetUserAttributesCmd_EmailNotFound verifies a clear error (and no PATCH)
// when no user matches the email.
func TestSetUserAttributesCmd_EmailNotFound(t *testing.T) {
	ts := scimListServer(t, []map[string]interface{}{}, nil)
	defer ts.Close()
	setEmailLookupEnv(t, ts.URL)

	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error {
		t.Fatal("exec must not be called when lookup finds nothing")
		return nil
	})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"missing@example.com", "--attr", "region=us-east"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no user found") {
		t.Fatalf("err = %v, want it to contain 'no user found'", err)
	}
}

// TestSetUserAttributesCmd_EmailAmbiguous verifies that multiple matches abort
// rather than guessing which user to modify.
func TestSetUserAttributesCmd_EmailAmbiguous(t *testing.T) {
	ts := scimListServer(t, []map[string]interface{}{
		{"id": "id-1", "userName": "dup@example.com"},
		{"id": "id-2", "userName": "dup@example.com"},
	}, nil)
	defer ts.Close()
	setEmailLookupEnv(t, ts.URL)

	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error {
		t.Fatal("exec must not be called on an ambiguous lookup")
		return nil
	})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"dup@example.com", "--attr", "region=us-east"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "multiple users found") {
		t.Fatalf("err = %v, want it to contain 'multiple users found'", err)
	}
}

// TestSetUserAttributesCmd_RequiresArg verifies the command rejects invocation
// without a user-id positional argument.
func TestSetUserAttributesCmd_RequiresArg(t *testing.T) {
	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error { return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--attr", "region=us-east"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no user-id provided")
	}
}

// TestSetUserAttributesCmd_RequiresAttr verifies an error when neither --attr
// nor --attr-json is supplied.
func TestSetUserAttributesCmd_RequiresAttr(t *testing.T) {
	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error { return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"user-123"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no attributes provided")
	}
}

// TestSetUserAttributesCmd_Schema verifies the hand-written command answers
// --schema with the same document shape as generated commands: no positional
// arg, no attributes, no API call.
func TestSetUserAttributesCmd_Schema(t *testing.T) {
	var called bool
	cmd := setUserAttributesCmd(func(req openapi.APIRequest) error { called = true; return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--schema"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --schema: %v\n%s", err, buf.String())
	}
	if called {
		t.Error("executor was called on a --schema run")
	}

	var doc openapi.SchemaDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema output: %v\n%s", err, buf.String())
	}
	if doc.Method != "PATCH" || doc.Path != "/api/scim/v2/Users/{id}" {
		t.Errorf("method/path = %q %q, want PATCH /api/scim/v2/Users/{id}", doc.Method, doc.Path)
	}
	if len(doc.Args) != 1 || doc.Args[0].Placeholder != "<user-id-or-email>" {
		t.Errorf("args = %+v, want the <user-id-or-email> positional", doc.Args)
	}
	body, ok := doc.Body.(map[string]interface{})
	if !ok {
		t.Fatalf("body is not an object: %T", doc.Body)
	}
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("body.properties missing: %v", body)
	}
	for _, key := range []string{"schemas", "Operations"} {
		if _, ok := props[key]; !ok {
			t.Errorf("body properties missing %q", key)
		}
	}
	if doc.Response == nil || doc.Response.Status != "200" || doc.Response.Schema == nil {
		t.Errorf("response = %+v, want the 200 SCIM user shape", doc.Response)
	}
}

// TestAddUserCommands_AttachesToUsers verifies addUserCommands finds the
// generated "users" group and adds set-attributes to it.
func TestAddUserCommands_AttachesToUsers(t *testing.T) {
	root := &cobra.Command{Use: "omni"}
	root.AddCommand(&cobra.Command{Use: "users"})

	addUserCommands(root, func(req openapi.APIRequest) error { return nil })

	usersCmd, _, _ := root.Find([]string{"users"})
	found := false
	for _, cmd := range usersCmd.Commands() {
		if cmd.Name() == "set-attributes" {
			found = true
			break
		}
	}
	if !found {
		t.Error("set-attributes not found under users command")
	}
}

// TestAddUserCommands_NoUsersGroup verifies addUserCommands is a no-op when
// there's no "users" command group.
func TestAddUserCommands_NoUsersGroup(t *testing.T) {
	root := &cobra.Command{Use: "omni"}
	addUserCommands(root, func(req openapi.APIRequest) error { return nil })

	if len(root.Commands()) != 0 {
		t.Errorf("expected no commands added, got %d", len(root.Commands()))
	}
}
