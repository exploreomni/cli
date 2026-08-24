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

### Create a branch of a model
  omni models create-branch <model-id> --name "my-branch"

### Run a semantic query directly
  omni query run --body '{"modelId":"MODEL_ID","query":{"fields":["view_name/field_name"],"limit":100}}'

### List dashboards
  omni content list --compact
  # Returns all content (dashboards, workbooks, folders). Dashboards have "hasDashboard": true.

### Download a dashboard
  omni dashboards download <dashboard-id>

### Read or edit a document (v2)
  # Read live state. The response is a valid draft PATCH body (round-trip design).
  omni documents v2-get <identifier> --compact

  # Edit metadata with flags (no JSON needed), then publish the draft:
  omni documents v2-patch-draft <identifier> --name "Q3 Revenue" --summary "rename"
  omni documents v2-publish-draft <identifier>

  # Edit content by round-tripping the full state through a file:
  omni documents v2-get <identifier> > doc.json
  # ...edit doc.json (containers, controls, queryPresentations, settings)...
  omni documents v2-patch-draft <identifier> --body - < doc.json
  omni documents v2-publish-draft <identifier>

  # Create a brand-new document (published immediately):
  omni documents v2-create <model-id> "My Dashboard"

### Search Omni documentation
  omni ai search-omni-docs --body '{"query":"how do I..."}'

## Command Groups
  ai            AI-powered query generation, jobs, doc search
  ai-eval       AI eval prompt set management
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
  unstable      Unstable/preview commands (document import/export)
  user-attributes  User attribute definitions
  users         User/group role management, set user attribute values
  config        CLI configuration profiles

## Discovering request and response shapes
Every API command accepts --schema, including GET/DELETE commands that take no
body. It prints the full call contract and exits without making an API call (no
token or positional args needed):
  - args:        positional args, with type and description
  - queryParams: query flags, with type, enum, required, description
  - body:        the request body's JSON schema plus a filled-in example
                 (null when the operation takes no body)
  - response:    the success response's shape (status, content type, schema)
Use it instead of guessing the JSON for --body, or the shape of a response you
are about to parse.
  omni models list --schema      # no body: args, query flags, response shape
  omni query run --schema
  omni connections create --schema --compact

Deeply nested shapes (e.g. documents v2-create) can be large. Narrow the
output with --depth N for a shallow overview, then --field PATH to drill into
one part of the request body. PATH is dotted and auto-descends through arrays
and maps, so you can name a leaf without knowing the container shape:
  omni documents v2-create --schema --depth 1                       # top-level overview
  omni documents v2-create --schema --field queryPresentations.data # just the tiles map
  omni documents v2-create --schema --field queryPresentations.data.query

## Common Flags
  --compact       Non-indented JSON output
  --token TOKEN   API token (overrides env/config)
  --base-url URL  API base URL (overrides config)
  --profile NAME  Config profile to use
  --body JSON     Request body (JSON string or "-" for stdin)
  --schema        Print args, flags, body schema + example, and response shape,
                  then exit (works on every API command)
  --field PATH    With --schema: drill into a dotted field path of the body
  --depth N       With --schema: cap nesting depth (lower = smaller)

## Tips
- Use "omni ai generate-query" to answer data questions — it picks fields and filters for you.
- Set a user's attribute values: omni users set-attributes <user-id> --attr region=us-east
- Path parameters are positional args: omni dashboards download <dashboard-id>
- Query parameters are flags: omni models list --pagesize 10
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
