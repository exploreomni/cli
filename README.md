# Omni CLI

Command-line tool for the Omni API. Commands are auto-generated from the OpenAPI spec at build time — no hand-written endpoint wrappers needed.

## Installation

### Homebrew (macOS / Linux) Preferred

```bash
brew tap exploreomni/tap
brew install omni
```
### Install script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/exploreomni/cli/main/install.sh | sh
```

This downloads the latest release, verifies the SHA-256 checksum, and installs the `omni` binary to `/usr/local/bin` (or `~/.local/bin` if `/usr/local/bin` isn't writable).

### Download from GitHub Releases

Pre-built binaries for macOS, Linux, and Windows are available on the [Releases page](https://github.com/exploreomni/cli/releases). Download the archive for your platform, extract it, and place the `omni` binary somewhere on your `PATH`.

| Platform | Architectures |
|----------|---------------|
| macOS    | amd64, arm64  |
| Linux    | amd64, arm64  |
| Windows  | amd64         |

### Build from source

```bash
git clone https://github.com/exploreomni/cli.git
cd cli
make build
```

The binary is written to `./bin/omni`.

### Updates

In an interactive terminal, Omni waits 24 hours after a successful check for a
newer stable release and prints an upgrade notice after a successful command.
A failed or interrupted check backs off for 15 minutes rather than retrying on
the next command, and concurrent runs coordinate through a lock file in the
cache directory, so several shells make one request between them. The check is
best-effort and is skipped in CI and whenever output isn't human-formatted on a
terminal. Set `OMNI_NO_UPDATE_NOTIFIER=1` to disable it.

To check explicitly, including from an agent or script:

```bash
omni update check --format json
```

## Quick start

### Configure a profile

```bash
omni config init
```

This creates a profile with your organization, API endpoint, and API key. You can create multiple profiles for different orgs or environments.

### Set your API token

Omni supports two types of API tokens:

- **Organization-wide tokens** — shared tokens scoped to an entire org
- **Personal access tokens (PATs)** — tokens tied to an individual user

Either enter your token during `config init`, or set the environment variable:

```bash
export OMNI_API_TOKEN=omni_osk_...
```

### Run a command

```bash
omni models list
omni documents list
omni --help
```

### Upload a CSV

Multipart request fields are generated as normal CLI flags. Binary OpenAPI
fields accept a local file path:

```bash
omni uploads create \
  --file ./people.csv \
  --model-id 00000000-0000-0000-0000-000000000000 \
  --view-name people
```

Multipart commands also accept `--body` JSON for compatibility. Values for
binary fields are interpreted as file paths:

```bash
omni uploads create --body '{"file":"./people.csv","modelId":"00000000-0000-0000-0000-000000000000"}'
```

## Shell completions

`omni` supports tab completion for bash, zsh, fish, and PowerShell. Pick your shell below and run the snippet once — tab completion works on every new shell thereafter.

### zsh

Source on every shell start (simplest):

```bash
echo 'source <(omni completion zsh)' >> ~/.zshrc
```

Or install into your fpath for faster startup:

```bash
omni completion zsh > "${fpath[1]}/_omni"
# ensure `autoload -U compinit && compinit` runs in your .zshrc
```

### bash

Requires the `bash-completion` package.

```bash
echo 'source <(omni completion bash)' >> ~/.bashrc
```

System-wide install (Linux):

```bash
omni completion bash | sudo tee /etc/bash_completion.d/omni > /dev/null
```

### fish

```bash
omni completion fish > ~/.config/fish/completions/omni.fish
```

### PowerShell

```powershell
omni completion powershell | Out-String | Invoke-Expression
```

Add that line to your PowerShell profile to persist it across sessions.

After installing, restart your shell and try: `omni <TAB>`, `omni ai <TAB>`, `omni ai sea<TAB>` → `omni ai search-omni-docs`.

## How it works

The CLI embeds the OpenAPI spec (`api/openapi.json`) into the binary. At startup it parses the spec and generates cobra subcommands for every operation. Each API tag becomes a command group, path params become positional args, query params become flags, and JSON request bodies are passed via `--body` or stdin. For `multipart/form-data` bodies, top-level schema properties become flags and binary properties are read from file paths.

A request body can be given three ways: inline JSON (`--body '{"name":"x"}'`), a file
(`--body @path/to/body.json`), or stdin (`--body - < path/to/body.json`). The body is
checked as JSON before the request is sent, so a typo fails locally instead of coming
back as a generic API 400.

Endpoints whose request body is `multipart/form-data` (`uploads create`,
`uploads replace-data`) take the same `--body` forms: the JSON is a map of field
values, checked locally and then framed into form parts, with binary fields given
as file paths. Their schema fields are also exposed as flags — see
[Upload a CSV](#upload-a-csv).

Flag names are always kebab-case, whatever the spec calls the parameter (`branchId` and `branch_id` both become `--branch-id`). Spelling is forgiving: case, dashes and underscores are ignored when matching, so `--branch-id`, `--branchId`, `--branch_id` and `--branchid` all set the same flag. `--help` shows the canonical form.

A query parameter whose name would collide with a global or built-in flag (`--token`, `--base-url`, `--body`, `--schema`, ...) is registered with a `param-` prefix instead — a spec parameter named `baseUrl` becomes `--param-base-url`, so `--base-url` keeps meaning the API endpoint. `--help` notes the rename, and the value is still sent under the spec's own parameter name.

Adding a new API endpoint requires no code changes — update `api/openapi.json` (or run `make sync-spec`) and rebuild.

## Auth

Auth is resolved with this precedence (highest wins):

1. `--token` flag
2. `OMNI_API_TOKEN` env var
3. Profile's `apiKey` from config file

Config file lives at `~/.config/omni-cli/config.json`.

## Output

All output is JSON to stdout. Errors go to stderr as JSON. Use `--compact` for non-indented output (good for piping to `jq`).

Failures write nothing to stdout — the API's error body, the error message, and any subcommand suggestions go to stderr, and the exit code is non-zero. An empty stdout therefore always means "no data", which keeps `omni ... | jq` from choking on error JSON.

A failed API call leaves exactly one JSON document on stderr, so `omni ... 2>err.json` stays parseable:

```json
{
  "error": "bad model id",
  "status": 400,
  "body": { "detail": "bad model id", "code": "INVALID" }
}
```

`body` holds the API's own payload and is omitted when the response wasn't JSON. A **successful** response that isn't JSON — `query run` streams `text/ndjson`, and returns CSV or XLSX with a result type — is passed through to stdout unchanged.

### Query results

`query run` and `query wait` stream results as NDJSON with the rows as Arrow. In JSON mode that stream passes through untouched. In human mode the CLI decodes it and renders what the model says about each field — its label, whether it's a dimension or a measure, and its number format — so a `NUMBER_0` measure reads `12,526` and a `percent` measure reads `39.92%`, in the query's column order. If the first response's wait window elapses, the CLI polls `query/wait` until every job has finished.

```
╭───────────────┬──────────┬────────────────────╮
│ Country       │ Sessions │ Engaged Sessions % │
├───────────────┼──────────┼────────────────────┤
│ United States │   12,526 │             39.92% │
│ Ireland       │      838 │             44.87% │
╰───────────────┴──────────┴────────────────────╯
```

### Charts

`--chart` draws the same results as the Omni app's bar table: every dimension is a column, and every measure gets a column of bars scaled to its own maximum, as the model defines them.

```bash
omni query run --body @revenue-by-category.json --chart --workbook
```

```
Category                     Total Sale Price
Jeans                            1,602,513.81 ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
Accessories                        955,617.30 ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
Outerwear & Coats                  842,064.07 ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
Fashion Hoodies & Sweatshir…       756,824.63 ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
Open in Omni: https://myorg.omniapp.co/e/1:abc123/1
```

Narrow it to one measure or one dimension by field or label — `--chart-value engaged_sessions_percent`, `--chart-label "Country"`, `events_ext.sessions` and `sessions` all work — and cap the row count with `--chart-rows`. `--workbook` also opens the query in an ephemeral workbook: the link prints under the output (or, in JSON mode, as `{"workbookUrl": …}` on stderr, since stdout stays the API's payload).

A query with `pivots` renders pivoted, as a table and as a chart: the remaining dimensions stay as rows, each pivot value heads its own columns, and a measure's bars share one scale across all of them. Columns that don't fit the terminal are dropped with a note.

```
Stage  Closed Lost                Closed Won               Negotiation
Region Total amount
AMER    $13,966,500 ████████████  $3,903,000 ███▍          $167,500 ▏
EMEA     $8,482,500 ███████▎      $1,949,500 █▋                   -
APAC     $3,919,500 ███▍          $1,591,500 █▍            $177,000 ▏
```

Values that cross zero get a zero axis rather than being scaled against the maximum:

```
Created At Month Mom Change
Feb 2024           2,793.82  │▇▇▇▇▇
Mar 2024          10,528.18  │▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
Apr 2024             -68.70 ▇│
Nov 2024          29,857.87  │▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇
```

A chart is drawn from the query stream's field metadata. A `resultType` in the body would replace that stream with a document, so `--chart` drops it and says so. Flags that can't work are refused before any request is made; a `--chart-value` or `--chart-label` is matched against the result's columns once it arrives.

```console
$ omni query run --body @q.json --chart          # body sets resultType
note: --chart ignores "resultType": "csv" and reads the query stream

$ omni query run --body @q.json --chart --format json    # or OMNI_OUTPUT_FORMAT=json
Error: --chart cannot be combined with JSON output: a chart is not JSON

$ omni models list --chart
Error: --chart plots query results: use it with query run or query wait
```

Piping is fine — `omni ... --chart | less` still draws, since that JSON is auto-detected rather than asked for. Off a terminal — piped to a file, `pbcopy`, or a Slack message — the chart draws at 80 columns, which fits a code block.

| Flag | Description |
|------|-------------|
| `--chart[=bar]` | Draw query results as a bar table |
| `--chart-value FIELD` | Only this measure, by field name or label (default: every measure) |
| `--chart-label FIELD` | Only this dimension as the row label (default: every dimension) |
| `--chart-rows N` | Most rows to draw before summarising the rest (default 50) |
| `--chart-style S` | `bar` (default — a hairline between rows), `block` (solid, with eighth-cell precision at the end), `line`, or `fill` (the value painted inside the bar; solid blocks without color) |
| `--workbook` | Also open the query in an ephemeral workbook and print its link |

## Environment variables

| Variable | Description |
|----------|-------------|
| `OMNI_API_TOKEN` | API token for authentication |

## Development

```bash
make build       # Build the binary
make test        # Run tests
make sync-spec   # Update spec from monorepo
make clean       # Remove built binary
```
