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
ts/                        # Legacy TypeScript CLI (preserved)
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

Auth is resolved with this precedence (highest wins):
1. `--token` flag
2. `OMNI_API_TOKEN` env var
3. `OMNI_API_KEY` env var
4. Profile's `apiKey` from config file

Config file lives at `~/.config/omni-cli/config.json` (compatible with the TS CLI format).

## Output

All output is JSON to stdout. Errors go to stderr as JSON. Use `--compact` for non-indented output (good for piping to `jq`).
