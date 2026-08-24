package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
)

func addBranchCommands(root *cobra.Command, exec openapi.Executor) {
	// Find the existing "models" command group (created by GenerateCommands)
	var modelsCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "models" {
			modelsCmd = cmd
			break
		}
	}
	if modelsCmd == nil {
		return
	}

	modelsCmd.AddCommand(createBranchCmd(exec))
}

func createBranchCmd(exec openapi.Executor) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-branch <model-id>",
		Short: "Create a branch of a model",
		Long:  "Create a new branch of an existing model. The model-id is the base model to branch from.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			baseModelID := args[0]
			name, _ := cmd.Flags().GetString("name")

			// Look up the model to get its connectionId
			cfg, err := resolveConfig(cmd)
			if err != nil {
				return err
			}

			resp, err := auth.Do(cfg, "GET", "/api/v1/models?modelId="+baseModelID, nil)
			if err != nil {
				return fmt.Errorf("looking up model: %w", err)
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("reading model response: %w", err)
			}

			var modelResp struct {
				Records []struct {
					ConnectionID string `json:"connectionId"`
				} `json:"records"`
			}
			if err := json.Unmarshal(respBody, &modelResp); err != nil {
				return fmt.Errorf("parsing model response: %w", err)
			}
			if len(modelResp.Records) == 0 {
				return fmt.Errorf("model %s not found", baseModelID)
			}

			connectionID := modelResp.Records[0].ConnectionID

			body := map[string]interface{}{
				"modelKind":    "BRANCH",
				"baseModelId":  baseModelID,
				"connectionId": connectionID,
			}
			if name != "" {
				body["modelName"] = name
			}

			bodyBytes, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshaling request body: %w", err)
			}

			return exec(openapi.APIRequest{
				Cmd:    cmd,
				Method: "POST",
				Path:   "/api/v1/models",
				Body:   bodyBytes,
			})
		},
	}

	cmd.Flags().String("name", "", "name for the new branch")

	// This command is hand-written rather than generated, but agents reach for
	// --schema on any command; describe the body it assembles so the flag works
	// here too. The body is static (the CLI fills modelKind and looks up
	// connectionId itself), so --field has nothing to drill into.
	openapi.RegisterSchemaFlag(cmd, func(c *cobra.Command, names openapi.SchemaFlags) error {
		if field, _ := c.Flags().GetString(names.Field); field != "" {
			return fmt.Errorf("--%s is not supported for create-branch; its request body is assembled by the CLI and shown in full", names.Field)
		}
		return openapi.EmitSchemaDoc(c, createBranchSchemaDoc())
	})

	return cmd
}

// createBranchSchemaDoc mirrors the generated commands' --schema document for the
// hand-written create-branch command. The body shown is what the CLI sends to
// POST /api/v1/models: modelKind is fixed to BRANCH, connectionId is resolved
// from the base model, and modelName comes from --name.
func createBranchSchemaDoc() openapi.SchemaDoc {
	str := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	return openapi.SchemaDoc{
		Method: "POST",
		Path:   "/api/v1/models",
		Args: []openapi.SchemaArg{{
			Name:        "modelId",
			Placeholder: "<model-id>",
			Type:        "string",
			Description: "ID of the base model to branch from",
		}},
		Required: []string{"modelKind", "baseModelId", "connectionId"},
		Body: map[string]interface{}{
			"type":        "object",
			"description": "Assembled by the CLI from <model-id> and --name; not passed through as JSON.",
			"properties": map[string]interface{}{
				"modelKind":    str(`always "BRANCH" for this command`),
				"baseModelId":  str("the <model-id> positional arg"),
				"connectionId": str("looked up from the base model; not settable"),
				"modelName":    str("the --name flag; omitted when --name is unset"),
			},
			"required": []string{"modelKind", "baseModelId", "connectionId"},
		},
		Example: map[string]interface{}{
			"modelKind":    "BRANCH",
			"baseModelId":  "<model-id>",
			"connectionId": "<resolved by the CLI>",
			"modelName":    "<name>",
		},
		Response: &openapi.SchemaResponse{
			Status:      "200",
			ContentType: "application/json",
			Description: "Model created successfully",
			Schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"success": map[string]interface{}{"type": "boolean", "description": "Whether the operation succeeded"},
					"error":   str("Error message if creation failed"),
					"message": str("Additional message"),
					"model": map[string]interface{}{
						"type":        "object",
						"description": "Created model details",
						"properties": map[string]interface{}{
							"id":        map[string]interface{}{"type": "string", "format": "uuid", "description": "Created model ID"},
							"modelKind": str("Kind of model created"),
							"name":      map[string]interface{}{"type": "string | null", "description": "Model name"},
						},
						"required": []string{"id", "modelKind", "name"},
					},
				},
				"required": []string{"success"},
			},
		},
	}
}
