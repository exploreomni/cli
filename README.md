# Omni CLI

Command-line tool for the Omni API. Commands are auto-generated from the OpenAPI spec at build time — no hand-written endpoint wrappers needed.

## Quick start

### Build

```bash
make build
```

### Configure a profile

```bash
./bin/omni config init
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
./bin/omni models list
./bin/omni dashboards list
./bin/omni --help
```

## How it works

The CLI embeds the OpenAPI spec (`api/openapi.json`) into the binary. At startup it parses the spec and generates cobra subcommands for every operation. Each API tag becomes a command group, path params become positional args, query params become flags, and request bodies are passed via `--body` or stdin.

Adding a new API endpoint requires no code changes — update `api/openapi.json` (or run `make sync-spec`) and rebuild.

## Auth

Auth is resolved with this precedence (highest wins):

1. `--token` flag
2. `OMNI_API_TOKEN` env var
3. Profile's `apiKey` from config file

Config file lives at `~/.config/omni-cli/config.json`.

## Output

All output is JSON to stdout. Errors go to stderr as JSON. Use `--compact` for non-indented output (good for piping to `jq`).

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
