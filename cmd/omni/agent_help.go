package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const agentHelpText = `# Omni CLI — Agent Guide

## Command Structure
  omni <group> <command> [positional-args] [--flags]

All output is JSON to stdout. Errors are JSON to stderr.
Use --compact for non-indented output (good for piping to jq).

## Auth
Set OMNI_API_TOKEN env var, or run: omni config init

## Common Workflows

### Answer a question about your data (recommended first approach)
  omni ai generate-query --body '{"modelId":"MODEL_ID","prompt":"how many users","executeQuery":true}'

### List models (to find model IDs)
  omni models list --compact

### Run a semantic query directly
  omni query run --body '{"modelId":"MODEL_ID","query":{"fields":["view_name/field_name"],"limit":100}}'

### List dashboards
  omni dashboards download <dashboard-id>

### Search Omni documentation
  omni ai search-omni-docs --body '{"query":"how do I..."}'

## Command Groups
  ai            AI-powered query generation, jobs, doc search
  connections   Manage database connections
  content       List content across the org
  dashboards    Download dashboards, manage filters
  documents     Create, list, and manage documents
  embed         Embed management
  folders       Folder operations
  labels        Label management
  models        List models, manage topics/views/fields, YAML
  query         Execute and wait for semantic queries
  scim          SCIM user/group provisioning
  schedules     Manage delivery schedules
  uploads       Upload and manage CSV files
  user-attributes  User attribute definitions
  users         User and group role management
  config        CLI configuration profiles

## Common Flags
  --compact       Non-indented JSON output
  --token TOKEN   API token (overrides env/config)
  --base-url URL  API base URL (overrides config)
  --profile NAME  Config profile to use
  --body JSON     Request body (JSON string or "-" for stdin)

## Tips
- Use "omni ai generate-query" to answer data questions — it picks fields and filters for you.
- Path parameters are positional args: omni models get <model-id>
- Query parameters are flags: omni models list --page-size 10
- Run "omni <group> --help" to see all commands in a group.
- Run "omni <group> <command> --help" to see flags for a specific command.
`

func addAgentHelpCommand(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "agent-help",
		Short: "Print agent-oriented usage guide",
		Long:  "Prints a concise guide for AI agents to quickly discover and use omni CLI commands.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(agentHelpText)
			return nil
		},
	})
}
