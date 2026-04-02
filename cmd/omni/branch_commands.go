package main

import (
	"encoding/json"
	"fmt"

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

			body := map[string]interface{}{
				"modelKind":   "BRANCH",
				"baseModelId": baseModelID,
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
