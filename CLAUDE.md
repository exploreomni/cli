# omni-cli

Go CLI for the Omni API. All commands are auto-generated from the OpenAPI 3.1 spec at build time — no hand-written endpoint wrappers needed.

## Architecture

The CLI embeds the OpenAPI spec (`api/openapi.json`) into the binary. At startup, it parses the spec with `libopenapi` and generates cobra subcommands for every operation. Each API tag becomes a command group, path params become positional args, query params become flags, and request bodies are passed via `--body` flag or stdin.

## Project Structure

```
cmd/omni/                  # Entry point + hand-written commands
  main.go                  # Root cobra command, spec loading, global flags
  config_commands.go       # config init/show/use (hand-written)
  agent_help.go            # omni agent-help — AI agents should run this first to learn the CLI
  output.go                # Response formatting
  openapi.json             # Embedded copy of spec (copied by Makefile)
internal/
  openapi/generate.go      # OpenAPI spec → cobra commands
  auth/auth.go             # Authenticated HTTP requests
  config/config.go         # Profile management, config resolution
  output/output.go         # JSON output helpers
api/
  openapi.json             # Source of truth OpenAPI spec
Makefile
```

## Development

```bash
make build                 # Build the binary
make sync-spec             # Update spec from monorepo
make test                  # Run tests
./omni --help              # See all commands
./omni models list --help  # See flags for a specific command
```

## Adding API Endpoints

You don't. Just update `api/openapi.json` (run `make sync-spec`) and rebuild. New endpoints appear as CLI commands automatically.

## Auth

API token is resolved with this precedence (highest wins):
1. `--token` flag
2. `OMNI_API_TOKEN` env var
3. Profile's `apiKey` from config file

Base URL is resolved with this precedence (highest wins):
1. `--base-url` flag
2. `OMNI_BASE_URL` env var
3. Profile's `apiEndpoint` from config file

The CLI refuses to send tokens over non-HTTPS or to unrecognized domains. Set `OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS=1` to bypass (e.g., local dev).

Config directory is resolved as: `OMNI_CONFIG_DIR` > `XDG_CONFIG_HOME/omni-cli` > `~/.config/omni-cli` (macOS/Linux) or `%AppData%/omni-cli` (Windows). The config file is `config.json` within that directory. Override the full path with `OMNI_CONFIG_PATH`.

## Output

All output is JSON to stdout. Errors go to stderr as JSON. Use `--compact` for non-indented output (good for piping to `jq`).

Nothing is written to stdout on failure: HTTP ≥400 bodies, error messages, and subcommand suggestions all go to stderr, and the exit code is non-zero. The one exception is a multi-job query stream where some jobs succeeded: the results that did decode are rendered and the failed jobs are reported on stderr, still exiting non-zero. A failed API call leaves exactly one JSON document on stderr — `{"error": <detail>, "status": <code>, "body": <the API's payload>}` — so `2>err.json` stays parseable; nothing else is printed alongside it. Runtime errors don't print the usage block (flag-parse errors still do).

`query run` and `query wait` stream NDJSON with rows as base64 Arrow. For `--format human` and `--chart` the CLI decodes that stream (`internal/result`) and renders from the model's field metadata in `summary.fields` — label, `is_dimension`, `data_type`, `format` — polling `query/wait` until every job finishes. Nothing is inferred from names or values. `--chart` (on API command groups only, with `--chart-value`, `--chart-rows`) draws the Omni app's bar table: every dimension a column, every measure a column of bars scaled to its own maximum; `--chart-value` narrows the bars to the measures it names, in that order (on a pivot, the measures spread across the pivot values). A query whose `model_job.pivots` names result columns is reshaped from the stream's long-form rows (`Set.Pivot`) for both the table and the chart — the other dimensions as rows, a column per pivot value and measure (pivot value over measure label), pivot values in the order the stream's row groups agree on, falling back to the pivot fields' own sort (descending if the query sorts them so), capped at `column_limit` — and a measure's bars share one scale across its pivot columns; a chart drops columns that don't fit the terminal, with a note (a table shows every column up to `column_limit`). It is accepted only on commands whose spec returns the stream (`query run`, `query wait`) and refused before any request otherwise; a `resultType` in the body is dropped (with a note in human mode) since those documents carry no metadata; an explicitly requested JSON format (`--format json`, `OMNI_OUTPUT_FORMAT`, or the profile's `defaultOutputFormat`) is an error, while the JSON a pipe auto-resolves to is not, so `--chart | less` draws. `--workbook` (same groups) sets `workbookUrl: true` on a body whose spec declares it — refused with `planOnly`, and refused rather than silently dropped when there is no JSON object to set it on — and surfaces the `X-Omni-Workbook-Url` header: a line under human-rendered output, or on stderr (plain for a passed-through CSV/XLSX, `{"workbookUrl": …}` in JSON mode) so stdout stays the payload. Human rendering strips control characters from API and warehouse text (cell values, labels, JSON strings and keys, error details, the workbook link) so a value can't drive the terminal; JSON output and passed-through payloads are untouched. Stream rendering is buffered and written once, and a failing `query/wait` poll reports through the same error envelope as any API call.

A 2xx body that isn't JSON (e.g. `query run`'s `text/ndjson` stream, or CSV/XLSX when `query run`'s body sets `"resultType"`) is passed through to stdout unchanged and counts as success. The body is read in full before anything is written, so a truncated response never leaves a partial payload on stdout.

A group command with no subcommand (`omni models`) prints its help to stderr and exits 1; an unknown subcommand errors with suggestions, `--help` or not (`omni models list-branches --help` is a typo, not a help request).
