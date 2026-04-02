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

// newTestServer returns an httptest.Server that fakes the models list endpoint
// (for connectionId lookup) and accepts any POST to /api/v1/models.
func newTestServer(t *testing.T, records []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{"records": records})
		case r.Method == "POST" && r.URL.Path == "/api/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		default:
			http.Error(w, "unexpected request", 500)
		}
	}))
}

// newTestRoot builds a minimal cobra root with the global flags that
// resolveConfig expects, wires up a models group, and attaches create-branch.
// Callers must also call t.Setenv for OMNI_API_TOKEN, OMNI_BASE_URL, and
// OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS.
func newTestRoot(exec openapi.Executor) *cobra.Command {
	root := &cobra.Command{Use: "omni"}
	root.PersistentFlags().StringP("profile", "p", "", "")
	root.PersistentFlags().String("token", "", "")
	root.PersistentFlags().String("base-url", "", "")
	root.PersistentFlags().Bool("compact", false, "")
	models := &cobra.Command{Use: "models"}
	models.AddCommand(createBranchCmd(exec))
	root.AddCommand(models)
	return root
}

// TestCreateBranchCmd_BodyWithName verifies that the command constructs the
// correct API request body including modelKind, baseModelId, connectionId
// (resolved from model lookup), and modelName from --name.
func TestCreateBranchCmd_BodyWithName(t *testing.T) {
	ts := newTestServer(t, []map[string]interface{}{
		{"connectionId": "conn-456"},
	})
	defer ts.Close()

	var captured openapi.APIRequest
	root := newTestRoot(func(req openapi.APIRequest) error {
		captured = req
		return nil
	})

	t.Setenv("OMNI_API_TOKEN", "test-token")
	t.Setenv("OMNI_BASE_URL", ts.URL)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	root.SetArgs([]string{"models", "create-branch", "model-123", "--name", "my-branch"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if body["modelKind"] != "BRANCH" {
		t.Errorf("modelKind = %v, want BRANCH", body["modelKind"])
	}
	if body["baseModelId"] != "model-123" {
		t.Errorf("baseModelId = %v, want model-123", body["baseModelId"])
	}
	if body["connectionId"] != "conn-456" {
		t.Errorf("connectionId = %v, want conn-456", body["connectionId"])
	}
	if body["modelName"] != "my-branch" {
		t.Errorf("modelName = %v, want my-branch", body["modelName"])
	}
}

// TestCreateBranchCmd_BodyWithoutName verifies that modelName is omitted from
// the request body when --name is not provided.
func TestCreateBranchCmd_BodyWithoutName(t *testing.T) {
	ts := newTestServer(t, []map[string]interface{}{
		{"connectionId": "conn-789"},
	})
	defer ts.Close()

	var captured openapi.APIRequest
	root := newTestRoot(func(req openapi.APIRequest) error {
		captured = req
		return nil
	})

	t.Setenv("OMNI_API_TOKEN", "test-token")
	t.Setenv("OMNI_BASE_URL", ts.URL)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	root.SetArgs([]string{"models", "create-branch", "model-456"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if _, exists := body["modelName"]; exists {
		t.Errorf("modelName should not be present when --name is not set, got %v", body["modelName"])
	}
	if body["modelKind"] != "BRANCH" {
		t.Errorf("modelKind = %v, want BRANCH", body["modelKind"])
	}
	if body["baseModelId"] != "model-456" {
		t.Errorf("baseModelId = %v, want model-456", body["baseModelId"])
	}
}

// TestCreateBranchCmd_PostsToCorrectPath verifies the request targets
// POST /api/v1/models.
func TestCreateBranchCmd_PostsToCorrectPath(t *testing.T) {
	ts := newTestServer(t, []map[string]interface{}{
		{"connectionId": "conn-abc"},
	})
	defer ts.Close()

	var captured openapi.APIRequest
	root := newTestRoot(func(req openapi.APIRequest) error {
		captured = req
		return nil
	})

	t.Setenv("OMNI_API_TOKEN", "test-token")
	t.Setenv("OMNI_BASE_URL", ts.URL)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	root.SetArgs([]string{"models", "create-branch", "model-abc"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if captured.Method != "POST" {
		t.Errorf("method = %q, want POST", captured.Method)
	}
	if captured.Path != "/api/v1/models" {
		t.Errorf("path = %q, want /api/v1/models", captured.Path)
	}
}

// TestCreateBranchCmd_ModelNotFound verifies the command returns an error when
// the model lookup returns no results.
func TestCreateBranchCmd_ModelNotFound(t *testing.T) {
	ts := newTestServer(t, []map[string]interface{}{})
	defer ts.Close()

	root := newTestRoot(func(req openapi.APIRequest) error { return nil })
	root.SilenceUsage = true
	root.SilenceErrors = true

	t.Setenv("OMNI_API_TOKEN", "test-token")
	t.Setenv("OMNI_BASE_URL", ts.URL)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	root.SetArgs([]string{"models", "create-branch", "nonexistent-id"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for model not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

// TestCreateBranchCmd_RequiresArg verifies the command rejects invocation
// without a model ID positional argument.
func TestCreateBranchCmd_RequiresArg(t *testing.T) {
	cmd := createBranchCmd(func(req openapi.APIRequest) error { return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no model-id provided")
	}
}

// TestAddBranchCommands_AttachesToModels verifies that addBranchCommands finds
// the existing "models" command group and adds create-branch to it.
func TestAddBranchCommands_AttachesToModels(t *testing.T) {
	root := &cobra.Command{Use: "omni"}
	root.AddCommand(&cobra.Command{Use: "models"})

	addBranchCommands(root, func(req openapi.APIRequest) error { return nil })

	modelsCmd, _, _ := root.Find([]string{"models"})
	found := false
	for _, cmd := range modelsCmd.Commands() {
		if cmd.Name() == "create-branch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("create-branch not found under models command")
	}
}

// TestAddBranchCommands_NoModelsGroup verifies that addBranchCommands is a
// no-op when there's no "models" command group.
func TestAddBranchCommands_NoModelsGroup(t *testing.T) {
	root := &cobra.Command{Use: "omni"}
	addBranchCommands(root, func(req openapi.APIRequest) error { return nil })

	if len(root.Commands()) != 0 {
		t.Errorf("expected no commands added, got %d", len(root.Commands()))
	}
}
