package main

import (
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

// TestBuildSetAttributesBody_StringAttrs verifies one replace op per --attr
// with the correct dotted path, string value, and PatchOp schema.
func TestBuildSetAttributesBody_StringAttrs(t *testing.T) {
	b, err := buildSetAttributesBody([]string{"region=us-east", "team=growth"}, "")
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	body := parseBody(t, b)

	if len(body.Schemas) != 1 || body.Schemas[0] != "urn:ietf:params:scim:api:messages:2.0:PatchOp" {
		t.Errorf("schemas = %v, want PatchOp", body.Schemas)
	}
	if len(body.Operations) != 2 {
		t.Fatalf("ops = %d, want 2", len(body.Operations))
	}
	// order preserved from --attr ordering
	if got := body.Operations[0]; got.Op != "replace" ||
		got.Path != "urn:omni:params:1.0:UserAttribute.region" || got.Value != "us-east" {
		t.Errorf("op[0] = %+v, want replace region=us-east", got)
	}
	if got := body.Operations[1]; got.Path != "urn:omni:params:1.0:UserAttribute.team" || got.Value != "growth" {
		t.Errorf("op[1] = %+v, want replace team=growth", got)
	}
}

// TestBuildSetAttributesBody_ClearAttr verifies an empty value clears the
// attribute via a replace op whose value is explicitly null (not omitted).
func TestBuildSetAttributesBody_ClearAttr(t *testing.T) {
	b, err := buildSetAttributesBody([]string{"region="}, "")
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	body := parseBody(t, b)
	if len(body.Operations) != 1 {
		t.Fatalf("ops = %d, want 1", len(body.Operations))
	}
	if body.Operations[0].Value != nil {
		t.Errorf("value = %v, want nil (null)", body.Operations[0].Value)
	}
	// the value key must be present and null, not omitted
	if !strings.Contains(string(b), `"value":null`) {
		t.Errorf("body should contain explicit null value, got %s", b)
	}
}

// TestBuildSetAttributesBody_AttrJSON verifies --attr-json contributes typed
// (numeric/array) values in deterministic (sorted-key) order.
func TestBuildSetAttributesBody_AttrJSON(t *testing.T) {
	b, err := buildSetAttributesBody(nil, `{"regions":["us","eu"],"level":3}`)
	if err != nil {
		t.Fatalf("buildSetAttributesBody: %v", err)
	}
	body := parseBody(t, b)
	if len(body.Operations) != 2 {
		t.Fatalf("ops = %d, want 2", len(body.Operations))
	}
	// sorted: "level" before "regions"
	if body.Operations[0].Path != "urn:omni:params:1.0:UserAttribute.level" {
		t.Errorf("op[0].path = %q, want level", body.Operations[0].Path)
	}
	if v, ok := body.Operations[0].Value.(float64); !ok || v != 3 {
		t.Errorf("level value = %v (%T), want number 3", body.Operations[0].Value, body.Operations[0].Value)
	}
	arr, ok := body.Operations[1].Value.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "us" || arr[1] != "eu" {
		t.Errorf("regions value = %v, want [us eu]", body.Operations[1].Value)
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
	body := parseBody(t, captured.Body)
	if len(body.Operations) != 1 || body.Operations[0].Value != "us-east" {
		t.Errorf("operations = %+v, want single region=us-east", body.Operations)
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
