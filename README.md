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
