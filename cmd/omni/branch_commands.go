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

	return cmd
}
