# omni-cli

Go CLI for the Omni API. All commands are auto-generated from the OpenAPI 3.1 spec at build time — no hand-written endpoint wrappers needed.

## Architecture

The CLI embeds the OpenAPI spec (`api/openapi.json`) into the binary. At startup, it parses the spec with `libopenapi` and generates cobra subcommands for every operation. Each API tag becomes a command group, path params become positional args, query params become flags, and request bodies are passed via `--body` flag or stdin.

## Project Structure

```
cmd/omni/                  # Entry point + config commands
  main.go                  # Root cobra command, spec loading, global flags
  config_commands.go       # config init/show/use (hand-written)
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
