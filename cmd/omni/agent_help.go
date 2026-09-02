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

Automatic update notices are disabled for non-interactive runs. Check explicitly:
  omni update check --format json --compact

## Streams and exit codes
On failure nothing is written to stdout — the API's error body, the error
message, and any suggestions all go to stderr, and the exit code is non-zero.
So an empty stdout always means "no data", never "parse this".

A failed API call leaves exactly one JSON document on stderr:
  {"error": "<message>", "status": 400, "body": {<the API's payload>}}
"body" is omitted when the response wasn't JSON. A successful response that
isn't JSON (query run streams text/ndjson) passes through to stdout unchanged.

A group with no subcommand ("omni models") prints its help to stderr and exits
1; an unrecognized subcommand ("omni models list-branches") is an error with
suggestions, with or without --help. Use "omni <group> --help" to see the real
subcommand names.

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
  # Read live state.
  omni documents v2-get <identifier> --compact

  # Edit metadata with flags (no JSON needed), then publish the draft:
  omni documents v2-patch-draft <identifier> --name "Q3 Revenue" --summary "rename"
  omni documents v2-publish-draft <identifier>

  # Edit content. A v2-get response is a STARTING POINT for a PATCH body, not a
  # guaranteed round-trip — read the caveats below before doing this:
  omni documents v2-get <identifier> > doc.json
  # ...edit doc.json (containers, controls, queryPresentations, settings)...
  omni documents v2-patch-draft <identifier> --body - < doc.json
  omni documents v2-get <identifier> --compact   # verify BEFORE publishing
  omni documents v2-publish-draft <identifier>

  # Create a brand-new document (published immediately):
  omni documents v2-create <model-id> "My Dashboard"

#### v2 PATCH caveats (do not skip — these cost real debugging time)
- A v2-get response is NOT accepted verbatim as a PATCH body. It carries
  read-only/derived keys the validator rejects, so expect to strip fields and
  retry: "hidden" on filter configs, and per-tile keys such as calculations,
  column_totals, fill_fields, pivots, row_totals, userEditedSQL, chartType,
  visType, fields, config. Read the error, drop the named key, retry.
- Send only the sections you are changing. Patch the smallest subtree that
  expresses the edit rather than replaying the whole document.
- Tiles (queryPresentations.data.<n>) and controls (controls.data.<id>) are
  REPLACE-WHOLE. Send the complete object for the entry you touch; a partial
  entry is rejected or silently loses the omitted fields.
- queryPresentations.order must enumerate EVERY presentation key, not just
  the ones you moved or added.
- A 200 does not mean the content survived. A successful patch can null out
  visConfig on tiles you did not intend to touch. Always re-read with v2-get
  and check the tiles you care about before v2-publish-draft.
- Use "omni documents v2-patch-draft --schema --field <path>" to learn the
  accepted shape of a subtree instead of guessing from the v2-get output.

### Search Omni documentation
  omni ai search-omni-docs --body '{"query":"how do I..."}'

### Upload a CSV
Multipart body fields are generated as flags; binary fields take file paths.
  omni uploads create --file ./people.csv --model-id MODEL_ID --view-name people

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
  update        Check for CLI updates

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

For multipart/form-data commands, top-level schema properties are also exposed
as flags. Binary properties are labeled "file path" in --help. --body remains
available; binary values in its JSON object are interpreted as file paths.

## Common Flags
  --compact       Non-indented JSON output
  --token TOKEN   API token (overrides env/config)
  --base-url URL  API base URL (overrides config)
  --profile NAME  Config profile to use
  --body JSON     Request body: JSON string, @path/to/file.json, or "-" for stdin
                  (a bare path is rejected client-side — prefix it with @)
                  For multipart/form-data endpoints (uploads create, uploads
                  replace-data) the JSON is a map of field values; binary
                  fields take file paths.
  --schema        Print args, flags, body schema + example, and response shape,
                  then exit (works on every API command)
  --field PATH    With --schema: drill into a dotted field path of the body
  --depth N       With --schema: cap nesting depth (lower = smaller)
  --query K=V     Extra query parameter not declared in the spec (repeatable)

## Tips
- Use "omni ai generate-query" to answer data questions — it picks fields and filters for you.
- Set a user's attribute values: omni users set-attributes <user-id> --attr region=us-east
- Path parameters are positional args: omni dashboards download <dashboard-id>
  Generated API commands describe every positional in an "Arguments:" section in
  their --help. Read it — some positionals take a NAME, not a UUID:
    omni models merge-branch <model-id> <branch-name>   # 2nd arg is the NAME
    omni models delete-branch <model-id> <branch-name>  # same: NAME, not the branch UUID
    omni labels get <name>                              # labels are addressed by name, not id
    omni documents add-label <identifier> <label-name>
    omni users get-model-roles <membership-id>          # membership ID, not the user ID
  The few hand-written commands (config *, agent-help, models create-branch,
  users set-attributes) have no Arguments section — read their Usage line.
- Query parameters are flags: omni models list --page-size 10
- Params the spec marks required are enforced before the request; a missing one fails locally.
- If the server demands a query param the spec doesn't declare, send it with --query key=value (repeatable).
- Flag names are kebab-case, and spelling is forgiving: case, dashes and
  underscores are ignored, so --branch-id, --branchId, --branch_id and
  --branchid all mean the same flag. --help always shows the canonical form.
- Promoted field flags (e.g. --name, --summary) are mutually exclusive with
  --body: using both is an error, not a merge. Pick one and put every field there.
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
