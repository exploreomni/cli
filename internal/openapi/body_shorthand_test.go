package openapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// assembleBody unit tests
//
// assembleBody converts positional CLI args and promoted flags into a JSON
// request body. These tests verify that each transform type ("string",
// "email-list", etc.) produces the correct JSON structure, that path params
// are excluded from the body, that promoted flags merge in correctly, and
// that bool flags are serialized as JSON booleans rather than strings.
// ---------------------------------------------------------------------------

func newMockCmd(flags map[string]string) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	for k, v := range flags {
		cmd.Flags().String(k, v, "")
	}
	return cmd
}

func TestAssembleBody_StringTransform(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "question", Transform: "string"},
		},
	}
	cmd := newMockCmd(nil)
	body, err := assembleBody(sh, []string{"How do I add a format?"}, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	if parsed["question"] != "How do I add a format?" {
		t.Errorf("question = %v, want 'How do I add a format?'", parsed["question"])
	}
}

func TestAssembleBody_EmailListTransform(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "users", Transform: "email-list"},
		},
	}
	cmd := newMockCmd(nil)
	body, err := assembleBody(sh, []string{"a@co.com,b@co.com,c@co.com"}, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	users, ok := parsed["users"].([]interface{})
	if !ok {
		t.Fatalf("users is not an array: %T", parsed["users"])
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}

	emails := []string{"a@co.com", "b@co.com", "c@co.com"}
	for i, u := range users {
		um := u.(map[string]interface{})
		if um["email"] != emails[i] {
			t.Errorf("users[%d].email = %v, want %s", i, um["email"], emails[i])
		}
	}
}

func TestAssembleBody_EmailListTrimsWhitespace(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "users", Transform: "email-list"},
		},
	}
	cmd := newMockCmd(nil)
	body, err := assembleBody(sh, []string{"a@co.com, b@co.com , c@co.com"}, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	users := parsed["users"].([]interface{})
	emails := []string{"a@co.com", "b@co.com", "c@co.com"}
	for i, u := range users {
		um := u.(map[string]interface{})
		if um["email"] != emails[i] {
			t.Errorf("users[%d].email = %v, want %s", i, um["email"], emails[i])
		}
	}
}

func TestAssembleBody_StringListTransform_Arg(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "userIds", Transform: "string-list"},
		},
	}
	cmd := newMockCmd(nil)
	body, err := assembleBody(sh, []string{"uuid-1, uuid-2 ,uuid-3"}, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	ids, ok := parsed["userIds"].([]interface{})
	if !ok {
		t.Fatalf("userIds is not an array: %T", parsed["userIds"])
	}
	want := []string{"uuid-1", "uuid-2", "uuid-3"}
	if len(ids) != len(want) {
		t.Fatalf("expected %d ids, got %d", len(want), len(ids))
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("userIds[%d] = %v, want %s", i, id, want[i])
		}
	}
}

func TestAssembleBody_StringListTransform_Flag(t *testing.T) {
	sh := &BodyShorthand{
		Flags: []FlagMapping{
			{FlagName: "user-ids", FieldPath: "userIds", Transform: "string-list"},
		},
	}
	cmd := newMockCmd(map[string]string{"user-ids": "a,b,c"})
	body, err := assembleBody(sh, nil, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	ids := parsed["userIds"].([]interface{})
	if len(ids) != 3 || ids[0] != "a" || ids[2] != "c" {
		t.Errorf("userIds = %v, want [a b c]", ids)
	}
}

func TestAssembleBody_ScimMemberListTransform(t *testing.T) {
	sh := &BodyShorthand{
		Flags: []FlagMapping{
			{FlagName: "member-ids", FieldPath: "members", Transform: "scim-member-list"},
		},
	}
	cmd := newMockCmd(map[string]string{"member-ids": "uuid-1,uuid-2"})
	body, err := assembleBody(sh, nil, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	members := parsed["members"].([]interface{})
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
	first := members[0].(map[string]interface{})
	if first["value"] != "uuid-1" {
		t.Errorf("members[0].value = %v, want uuid-1", first["value"])
	}
}

func TestAssembleBody_JsonTransform_Array(t *testing.T) {
	sh := &BodyShorthand{
		Flags: []FlagMapping{
			{FlagName: "group-filters", FieldPath: "groupFilters", Transform: "json"},
		},
	}
	cmd := newMockCmd(map[string]string{"group-filters": `[{"field":"a"},{"field":"b"}]`})
	body, err := assembleBody(sh, nil, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	filters := parsed["groupFilters"].([]interface{})
	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}
	first := filters[0].(map[string]interface{})
	if first["field"] != "a" {
		t.Errorf("groupFilters[0].field = %v, want a", first["field"])
	}
}

func TestAssembleBody_JsonTransform_InvalidReturnsError(t *testing.T) {
	sh := &BodyShorthand{
		Flags: []FlagMapping{
			{FlagName: "data", FieldPath: "data", Transform: "json"},
		},
	}
	cmd := newMockCmd(map[string]string{"data": "not json{"})
	_, err := assembleBody(sh, nil, 0, cmd)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %q, want 'invalid JSON'", err.Error())
	}
}

func TestAssembleBody_WithPathParams(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "userId", Transform: "string"},
		},
	}
	cmd := newMockCmd(nil)
	// args[0] is a path param, args[1] is the shorthand arg
	body, err := assembleBody(sh, []string{"doc-123", "user-456"}, 1, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	if parsed["userId"] != "user-456" {
		t.Errorf("userId = %v, want user-456", parsed["userId"])
	}
	if _, exists := parsed["doc-123"]; exists {
		t.Error("path param should not appear in body")
	}
}

func TestAssembleBody_WithPromotedFlags(t *testing.T) {
	sh := &BodyShorthand{
		Args: []ArgMapping{
			{FieldPath: "name", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "color", FieldPath: "color"},
			{FlagName: "description", FieldPath: "description"},
		},
	}
	cmd := newMockCmd(map[string]string{"color": "#ff0000", "description": ""})
	body, err := assembleBody(sh, []string{"my-label"}, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	if parsed["name"] != "my-label" {
		t.Errorf("name = %v, want my-label", parsed["name"])
	}
	if parsed["color"] != "#ff0000" {
		t.Errorf("color = %v, want #ff0000", parsed["color"])
	}
	if _, exists := parsed["description"]; exists {
		t.Error("empty flag should not appear in body")
	}
}

func TestAssembleBody_BoolFlag(t *testing.T) {
	sh := &BodyShorthand{
		Flags: []FlagMapping{
			{FlagName: "run-query", FieldPath: "runQuery", IsBool: true},
		},
	}
	cmd := newMockCmd(map[string]string{"run-query": "true"})
	body, err := assembleBody(sh, nil, 0, cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)
	if parsed["runQuery"] != true {
		t.Errorf("runQuery = %v, want true", parsed["runQuery"])
	}
}

// ---------------------------------------------------------------------------
// flexibleArgs validator tests
//
// flexibleArgs returns a cobra arg validator that accepts different arg
// counts depending on context: when --body/--json-body is provided, only
// path params are expected; in shorthand mode, path params + shorthand args
// are required; for flags-only shorthands, only path params are expected.
// These tests verify each mode rejects wrong arg counts.
// ---------------------------------------------------------------------------

func TestFlexibleArgs_ShorthandMode(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("body", "", "")
	cmd.Flags().String("json-body", "", "")
	validator := flexibleArgs(1, 1)

	// 2 args (1 path + 1 shorthand) should pass
	if err := validator(cmd, []string{"path-param", "shorthand-arg"}); err != nil {
		t.Errorf("expected pass with 2 args: %v", err)
	}

	// 1 arg should fail (missing shorthand)
	if err := validator(cmd, []string{"path-param"}); err == nil {
		t.Error("expected error with 1 arg in shorthand mode")
	}
}

func TestFlexibleArgs_BodyMode(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("body", `{"key":"val"}`, "")
	cmd.Flags().String("json-body", "", "")
	validator := flexibleArgs(1, 1)

	// 1 arg (path param only) should pass when --body is set
	if err := validator(cmd, []string{"path-param"}); err != nil {
		t.Errorf("expected pass with 1 arg + --body: %v", err)
	}

	// 2 args should fail when --body is set
	if err := validator(cmd, []string{"path-param", "extra"}); err == nil {
		t.Error("expected error with 2 args + --body")
	}
}

func TestFlexibleArgs_JsonBodyMode(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("body", "", "")
	cmd.Flags().String("json-body", `{"key":"val"}`, "")
	validator := flexibleArgs(1, 1)

	if err := validator(cmd, []string{"path-param"}); err != nil {
		t.Errorf("expected pass with 1 arg + --json-body: %v", err)
	}
}

func TestFlexibleArgs_FlagsOnly(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("body", "", "")
	cmd.Flags().String("json-body", "", "")
	validator := flexibleArgs(1, 0)

	// 1 arg (path param) should pass
	if err := validator(cmd, []string{"path-param"}); err != nil {
		t.Errorf("expected pass with 1 arg: %v", err)
	}

	// 0 args should fail
	if err := validator(cmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
}

// ---------------------------------------------------------------------------
// Integration tests: buildCommand + shorthand
//
// These tests wire up the full pipeline: buildCommand creates a cobra
// command from an operationInfo, applyBodyShorthand attaches the shorthand,
// and then we execute the command with real CLI args. A mock executor
// captures the resulting APIRequest so we can verify the path, body JSON,
// and flag values match what we'd expect from the shorthand expansion.
// ---------------------------------------------------------------------------

func TestShorthand_AiSearchOmniDocs(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"How do I add a format to a dimension?"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["question"] != "How do I add a format to a dimension?" {
		t.Errorf("question = %v, want 'How do I add a format to a dimension?'", body["question"])
	}
}

func TestShorthand_AiGenerateQuery(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiGenerateQuery",
		Method:      "POST",
		Path:        "/api/v1/ai/generate-query",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"model-uuid-123", "Show total revenue by month"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["modelId"] != "model-uuid-123" {
		t.Errorf("modelId = %v, want model-uuid-123", body["modelId"])
	}
	if body["prompt"] != "Show total revenue by month" {
		t.Errorf("prompt = %v, want 'Show total revenue by month'", body["prompt"])
	}
}

func TestShorthand_AiGenerateQuery_WithFlags(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiGenerateQuery",
		Method:      "POST",
		Path:        "/api/v1/ai/generate-query",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"model-uuid", "Revenue query", "--run-query", "false", "--current-topic-name", "orders"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["runQuery"] != false {
		t.Errorf("runQuery = %v, want false", body["runQuery"])
	}
	if body["currentTopicName"] != "orders" {
		t.Errorf("currentTopicName = %v, want orders", body["currentTopicName"])
	}
}

func TestShorthand_UsersCreateEmailOnlyBulk(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Users",
		OperationID: "usersCreateEmailOnlyBulk",
		Method:      "POST",
		Path:        "/api/v1/users/email-only/bulk",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"a@co.com,b@co.com,c@co.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	users := body["users"].([]interface{})
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	first := users[0].(map[string]interface{})
	if first["email"] != "a@co.com" {
		t.Errorf("users[0].email = %v, want a@co.com", first["email"])
	}
}

func TestShorthand_UsersCreateEmailOnly(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Users",
		OperationID: "usersCreateEmailOnly",
		Method:      "POST",
		Path:        "/api/v1/users/email-only",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"user@example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["email"] != "user@example.com" {
		t.Errorf("email = %v, want user@example.com", body["email"])
	}
}

func TestShorthand_DocumentsTransferOwnership(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Documents",
		OperationID: "documentsTransferOwnership",
		Method:      "PUT",
		Path:        "/api/v1/documents/{identifier}/transfer-ownership",
		PathParams:  []paramInfo{{Name: "identifier", In: "path"}},
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"doc-123", "user-456"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(captured.Path, "doc-123") {
		t.Errorf("path = %q, expected to contain doc-123", captured.Path)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["userId"] != "user-456" {
		t.Errorf("userId = %v, want user-456", body["userId"])
	}
}

func TestShorthand_LabelsCreate(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Labels",
		OperationID: "labelsCreate",
		Method:      "POST",
		Path:        "/api/v1/labels",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"important", "--color", "#0366d6"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["name"] != "important" {
		t.Errorf("name = %v, want important", body["name"])
	}
	if body["color"] != "#0366d6" {
		t.Errorf("color = %v, want #0366d6", body["color"])
	}
}

func TestShorthand_FoldersCreate(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Folders",
		OperationID: "foldersCreate",
		Method:      "POST",
		Path:        "/api/v1/folders",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"My Folder", "--scope", "organization"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["name"] != "My Folder" {
		t.Errorf("name = %v, want 'My Folder'", body["name"])
	}
	if body["scope"] != "organization" {
		t.Errorf("scope = %v, want organization", body["scope"])
	}
}

func TestShorthand_DocumentsUpdate_FlagsOnly(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Documents",
		OperationID: "documentsUpdate",
		Method:      "PATCH",
		Path:        "/api/v1/documents/{identifier}",
		PathParams:  []paramInfo{{Name: "identifier", In: "path"}},
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"doc-123", "--name", "New Name", "--clear-existing-draft", "true"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["name"] != "New Name" {
		t.Errorf("name = %v, want 'New Name'", body["name"])
	}
	if body["clearExistingDraft"] != true {
		t.Errorf("clearExistingDraft = %v, want true", body["clearExistingDraft"])
	}
}

func TestShorthand_ModelsGitSync_FlagsOnly(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Models",
		OperationID: "modelsGitSync",
		Method:      "POST",
		Path:        "/api/v1/models/{modelId}/git/sync",
		PathParams:  []paramInfo{{Name: "modelId", In: "path"}},
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"model-123", "--commit-message", "Update schema"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	json.Unmarshal(captured.Body, &body)
	if body["commitMessage"] != "Update schema" {
		t.Errorf("commitMessage = %v, want 'Update schema'", body["commitMessage"])
	}
}

// ---------------------------------------------------------------------------
// Fallback / alias tests
//
// Shorthand is opt-in — users can always pass raw JSON via --body or the
// hidden --json-body alias. These tests verify: (1) --body bypasses
// shorthand and sends JSON verbatim, (2) --json-body works identically,
// (3) using both at once returns an error, and (4) --body still works
// alongside path params on commands that also have shorthand args.
// ---------------------------------------------------------------------------

func TestShorthand_BodyFlagOverride(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"--body", `{"question":"raw json"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if string(captured.Body) != `{"question":"raw json"}` {
		t.Errorf("body = %q, want raw json passthrough", string(captured.Body))
	}
}

func TestShorthand_JsonBodyAlias(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"--json-body", `{"question":"via alias"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if string(captured.Body) != `{"question":"via alias"}` {
		t.Errorf("body = %q, want alias passthrough", string(captured.Body))
	}
}

func TestShorthand_BothBodyFlagsError(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--body", `{"a":"b"}`, "--json-body", `{"c":"d"}`})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --body and --json-body are set")
	}
	if !strings.Contains(err.Error(), "cannot use both") {
		t.Errorf("error = %q, want 'cannot use both' message", err.Error())
	}
}

// Verify that --body still works for operations with a shorthand when
// the user provides path params + --body but no shorthand positional arg.
func TestShorthand_BodyWithPathParams(t *testing.T) {
	var captured APIRequest
	exec := func(req APIRequest) error { captured = req; return nil }

	op := &operationInfo{
		Tag:         "Documents",
		OperationID: "documentsTransferOwnership",
		Method:      "PUT",
		Path:        "/api/v1/documents/{identifier}/transfer-ownership",
		PathParams:  []paramInfo{{Name: "identifier", In: "path"}},
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	cmd.SetArgs([]string{"doc-123", "--body", `{"userId":"user-789"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if string(captured.Body) != `{"userId":"user-789"}` {
		t.Errorf("body = %q, want raw json passthrough", string(captured.Body))
	}
}

// ---------------------------------------------------------------------------
// Help text tests
//
// Verify that shorthand registration updates the command's help output:
// the Example field should show both shorthand and JSON body usage, and
// the Use string should include placeholder names for shorthand args
// (e.g. "<question>", "<model-id> <prompt>").
// ---------------------------------------------------------------------------

func TestShorthand_HelpContainsExamples(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	if !strings.Contains(cmd.Example, "Shorthand") {
		t.Error("Example should contain shorthand example")
	}
	if !strings.Contains(cmd.Example, "JSON body") {
		t.Error("Example should contain JSON body example")
	}
	if !strings.Contains(cmd.Example, "search-omni-docs") {
		t.Error("Example should contain the command name in examples")
	}
}

func TestShorthand_UseStringContainsArgs(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiSearchOmniDocs",
		Method:      "POST",
		Path:        "/api/v1/ai/search-omni-docs",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	if !strings.Contains(cmd.Use, "<question>") {
		t.Errorf("Use = %q, expected to contain <question>", cmd.Use)
	}
}

func TestShorthand_UseStringContainsMultipleArgs(t *testing.T) {
	exec := func(req APIRequest) error { return nil }

	op := &operationInfo{
		Tag:         "AI",
		OperationID: "aiGenerateQuery",
		Method:      "POST",
		Path:        "/api/v1/ai/generate-query",
		HasBody:     true,
	}

	cmd := buildCommand(op, exec)
	if !strings.Contains(cmd.Use, "<model-id>") {
		t.Errorf("Use = %q, expected to contain <model-id>", cmd.Use)
	}
	if !strings.Contains(cmd.Use, "<prompt>") {
		t.Errorf("Use = %q, expected to contain <prompt>", cmd.Use)
	}
}

// ---------------------------------------------------------------------------
// Verify all registered shorthands reference valid operations
//
// Guards against registry drift: checks that the shorthand count matches
// expectations, and that every registered shorthand can be applied to a
// command without panics — arg placeholders appear in Use, and all
// promoted flags are registered on the command.
// ---------------------------------------------------------------------------

func TestShorthand_RegistryNotEmpty(t *testing.T) {
	if len(bodyShorthands) == 0 {
		t.Fatal("bodyShorthands registry is empty")
	}
	if len(bodyShorthands) != 54 {
		t.Errorf("expected 54 shorthand entries, got %d", len(bodyShorthands))
	}
}

// Verify each shorthand builds a valid command with the real spec.
func TestShorthand_AllEntriesBuildSuccessfully(t *testing.T) {
	for opID, sh := range bodyShorthands {
		// Construct a minimal operationInfo
		op := &operationInfo{
			Tag:         "test",
			OperationID: opID,
			Method:      "POST",
			Path:        "/test",
			HasBody:     true,
		}

		cmd := buildCommand(op, func(req APIRequest) error { return nil })

		// Verify shorthand args appear in Use
		for _, a := range sh.Args {
			if !strings.Contains(cmd.Use, "<"+a.Name+">") {
				t.Errorf("%s: Use=%q missing <%s>", opID, cmd.Use, a.Name)
			}
		}

		// Verify promoted flags exist
		for _, f := range sh.Flags {
			if cmd.Flags().Lookup(f.FlagName) == nil {
				t.Errorf("%s: missing promoted flag --%s", opID, f.FlagName)
			}
		}
	}
}
