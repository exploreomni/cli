package openapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const multipartTestSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "multipart test", "version": "1.0"},
  "paths": {
    "/uploads": {
      "post": {
        "operationId": "uploadsCreate",
        "tags": ["Uploads"],
        "requestBody": {
          "required": true,
          "content": {
            "multipart/form-data": {
              "schema": {
                "type": "object",
                "required": ["file", "modelId"],
                "properties": {
                  "file": {"type": "string", "format": "binary", "description": "CSV to upload"},
                  "modelId": {"type": "string", "description": "target model"},
                  "viewName": {"type": "string"},
                  "publish": {"type": "boolean"},
                  "labels": {"type": "array", "items": {"type": "string"}}
                }
              },
              "encoding": {
                "file": {"contentType": "text/csv"}
              }
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  }
}`

type capturedPart struct {
	fileName    string
	contentType string
	values      []string
}

func parseCapturedMultipart(t *testing.T, request APIRequest) map[string]capturedPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(request.ContentType)
	if err != nil {
		t.Fatalf("ParseMediaType(%q): %v", request.ContentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("media type = %q, want multipart/form-data", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("multipart content type has no boundary")
	}

	parts := map[string]capturedPart{}
	reader := multipart.NewReader(bytes.NewReader(request.Body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("ReadAll(%s): %v", part.FormName(), err)
		}
		got := parts[part.FormName()]
		got.fileName = part.FileName()
		got.contentType = part.Header.Get("Content-Type")
		got.values = append(got.values, string(data))
		parts[part.FormName()] = got
	}
	return parts
}

func TestGenerateCommands_MultipartFlagsAndBody(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(filePath, []byte("name\nAda\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var captured APIRequest
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		captured = request
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	command := commands[0].Commands()[0]
	for _, flag := range []string{"file", "model-id", "view-name", "publish", "labels"} {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("missing generated --%s flag", flag)
		}
	}

	for flag, value := range map[string]string{
		"file": filePath, "model-id": "model-123", "view-name": "people", "publish": "true", "labels": `["one","two"]`,
	} {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}
	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	parts := parseCapturedMultipart(t, captured)
	if got := parts["file"]; got.fileName != "people.csv" || got.contentType != "text/csv" || len(got.values) != 1 || got.values[0] != "name\nAda\n" {
		t.Errorf("file part = %#v", got)
	}
	if got := parts["modelId"].values; len(got) != 1 || got[0] != "model-123" {
		t.Errorf("modelId = %#v", got)
	}
	if got := parts["publish"].values; len(got) != 1 || got[0] != "true" {
		t.Errorf("publish = %#v", got)
	}
	if got := parts["labels"].values; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("labels = %#v", got)
	}
}

func TestGenerateCommands_MultipartJSONBody(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(filePath, []byte("id\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var captured APIRequest
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		captured = request
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	command := commands[0].Commands()[0]
	body, err := json.Marshal(map[string]interface{}{
		"file": filePath, "modelId": "from-body", "extra": map[string]bool{"ok": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Flags().Set("body", string(body)); err != nil {
		t.Fatal(err)
	}
	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	parts := parseCapturedMultipart(t, captured)
	if got := parts["modelId"].values; len(got) != 1 || got[0] != "from-body" {
		t.Errorf("modelId = %#v", got)
	}
	if got := parts["extra"]; got.contentType != "application/json" || len(got.values) != 1 || got.values[0] != `{"ok":true}` {
		t.Errorf("extra = %#v", got)
	}
}

func TestGenerateCommands_MultipartValidatesFlagInvocation(t *testing.T) {
	called := false
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command := commands[0].Commands()[0]
	if err := command.Flags().Set("model-id", "model-123"); err != nil {
		t.Fatal(err)
	}
	err = command.RunE(command, nil)
	if err == nil || !strings.Contains(err.Error(), `required multipart field "file"`) {
		t.Fatalf("error = %v, want missing file", err)
	}
	if called {
		t.Fatal("executor called for invalid multipart invocation")
	}
}

func TestRealSpec_UploadCommandsExposeFileFlags(t *testing.T) {
	commands, err := GenerateCommands(loadSpec(t), func(request APIRequest) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var uploadsFound int
	for _, group := range commands {
		if group.Name() != "uploads" {
			continue
		}
		for _, command := range group.Commands() {
			if command.Name() != "create" && command.Name() != "replace-data" {
				continue
			}
			uploadsFound++
			if command.Flags().Lookup("file") == nil {
				t.Errorf("uploads %s has no generated --file flag", command.Name())
			}
		}
	}
	if uploadsFound != 2 {
		t.Fatalf("found %d multipart upload commands, want 2", uploadsFound)
	}
}

// Multipart bodies are JSON too, so they get the same --body handling as JSON
// operations: @file reading and a client-side validity check.
func TestGenerateCommands_MultipartBodyFromFile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("id\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bodyPath := filepath.Join(dir, "body.json")
	body, err := json.Marshal(map[string]string{"file": csvPath, "modelId": "from-file"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	var captured APIRequest
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		captured = request
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateCommands: %v", err)
	}
	command := commands[0].Commands()[0]
	if err := command.Flags().Set("body", "@"+bodyPath); err != nil {
		t.Fatal(err)
	}
	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	parts := parseCapturedMultipart(t, captured)
	if got := parts["modelId"].values; len(got) != 1 || got[0] != "from-file" {
		t.Errorf("modelId = %#v", got)
	}
	if got := parts["file"]; got.fileName != "data.csv" {
		t.Errorf("file = %#v", got)
	}
}

func TestGenerateCommands_MultipartBodyPathHint(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(bodyPath, []byte(`{"modelId":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command := commands[0].Commands()[0]
	if err := command.Flags().Set("body", bodyPath); err != nil {
		t.Fatal(err)
	}
	err = command.RunE(command, nil)
	if err == nil || !strings.Contains(err.Error(), "looks like a file path") {
		t.Fatalf("error = %v, want file-path hint", err)
	}
	if called {
		t.Fatal("executor called for a path-shaped --body")
	}
}

// An explicitly empty --body is a body the caller asked for, so it gets the
// "omit the flag" error rather than reaching buildMultipartBody as a bare EOF.
func TestGenerateCommands_MultipartEmptyBodyFlag(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(filePath, []byte("name\nAda\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command := commands[0].Commands()[0]
	for flag, value := range map[string]string{"file": filePath, "model-id": "model-123", "body": ""} {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}

	err = command.RunE(command, nil)
	if err == nil || !strings.Contains(err.Error(), "--body is empty") {
		t.Fatalf("error = %v, want the empty --body message", err)
	}
	if called {
		t.Fatal("executor called for an empty --body")
	}
}

// Binary field values expand "~" the same way --body @path does; shells leave
// it alone inside a flag value.
func TestGenerateCommands_MultipartExpandsHomeInFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "people.csv"), []byte("name\nAda\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var captured APIRequest
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		captured = request
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command := commands[0].Commands()[0]
	for flag, value := range map[string]string{"file": "~/people.csv", "model-id": "model-123"} {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}
	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	parts := parseCapturedMultipart(t, captured)
	if got := parts["file"]; got.fileName != "people.csv" || len(got.values) != 1 || got.values[0] != "name\nAda\n" {
		t.Errorf("file part = %#v", got)
	}
}

// JSON "null" decodes into a nil map, which the flag merge would panic on.
func TestGenerateCommands_MultipartRejectsNullBody(t *testing.T) {
	called := false
	commands, err := GenerateCommands([]byte(multipartTestSpec), func(request APIRequest) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command := commands[0].Commands()[0]
	for flag, value := range map[string]string{"body": "null", "model-id": "model-123"} {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}

	err = command.RunE(command, nil)
	if err == nil || !strings.Contains(err.Error(), "JSON object of field values") {
		t.Fatalf("error = %v, want a non-object --body error", err)
	}
	if called {
		t.Fatal("executor called for a null --body")
	}
}

func TestParseMultipartFlagValue_ChecksTypeAndTrailingData(t *testing.T) {
	array := multipartFieldInfo{Name: "labels", Type: "array"}
	object := multipartFieldInfo{Name: "meta", Type: "object"}

	if _, err := parseMultipartFlagValue(array, `{"x":1}`); err == nil {
		t.Error("an object passed for an array field was accepted")
	}
	if _, err := parseMultipartFlagValue(object, `["a"]`); err == nil {
		t.Error("an array passed for an object field was accepted")
	}
	if _, err := parseMultipartFlagValue(array, `["a"] trailing`); err == nil {
		t.Error("trailing data after an array was accepted")
	}
	if _, err := parseMultipartFlagValue(array, `["a","b"]`); err != nil {
		t.Errorf("valid array rejected: %v", err)
	}
}

// Two fields whose flag names collide must not register the same pflag twice —
// pflag panics on a redefinition, taking down the whole CLI at startup.
func TestRegisterMultipartFlags_ResolvesCollisions(t *testing.T) {
	command := &cobra.Command{Use: "upload"}
	fields := []multipartFieldInfo{
		{Name: "body", FlagName: "body"},
		{Name: "Body", FlagName: "body"},
		{Name: "body_", FlagName: "body"},
	}

	registerMultipartFlags(command, fields)

	seen := map[string]bool{}
	for _, field := range fields {
		if field.FlagName == "body" {
			t.Errorf("field %q kept the reserved --body name", field.Name)
		}
		if seen[field.FlagName] {
			t.Errorf("duplicate flag name %q", field.FlagName)
		}
		seen[field.FlagName] = true
		if command.Flags().Lookup(field.FlagName) == nil {
			t.Errorf("flag --%s was not registered", field.FlagName)
		}
	}
}
