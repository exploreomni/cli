# Make omni-cli Agent-Friendly

## Context
The omni CLI's primary consumers are AI agents (Claude Code, ChatGPT, Cursor), not humans. The CLI already has good fundamentals (JSON output, noun-verb structure, OpenAPI-driven generation), but agents currently have to run `--help` on every command group to discover capabilities and can't introspect request/response schemas. We're adding four features to make the CLI a first-class agent tool.

## Changes

### 1. AGENTS.md (new file)
Create `/AGENTS.md` at project root — the cross-agent convention file that Claude Code, Cursor, etc. look for.

Content covers: quick start (auth, first command), command structure pattern, common workflows with concrete examples, output/error format, `omni openapi` and `omni mcp serve` references. Keep under 100 lines.

### 2. `omni openapi` command (~15 lines)
Add a hand-written command in `cmd/omni/main.go` that dumps the embedded OpenAPI spec to stdout. Lets agents read the full 85-endpoint API surface in one call.

- Add inline in `main()` as a closure capturing `specData` (same pattern as `executeAPICall`)
- Pretty-print by default, respect `--compact` flag
- Skip auth in `PersistentPreRunE` (add `"openapi"` to the skip list)

### 3. `--help-json` flag (~40 lines)
Add a persistent `--help-json` bool flag on root command. When set, any command outputs structured JSON describing itself instead of executing.

**Requires a shared refactor**: export `operationInfo`/`paramInfo` types from `internal/openapi/generate.go` and stash them on each generated command via `cmd.Annotations["operationInfo"]`.

Changes to `internal/openapi/generate.go`:
- Rename `operationInfo` → `OperationInfo`, `paramInfo` → `ParamInfo` (export them)
- In `buildCommand()`, serialize `OperationInfo` to JSON and store in `cmd.Annotations`
- Add `ParseOperations(specData []byte) ([]OperationInfo, error)` — extracted from `GenerateCommands` so MCP server can reuse the parsing without cobra dependency

Changes to `cmd/omni/main.go`:
- Add `--help-json` persistent flag
- Add `PersistentPreRun` check: if `--help-json` is set, read `cmd.Annotations["operationInfo"]`, output as JSON, exit
- For non-generated commands (config, openapi), output basic cobra metadata

JSON structure:
```json
{
  "command": "omni models list",
  "summary": "List all models",
  "description": "...",
  "method": "GET",
  "path": "/api/unstable/models",
  "args": [{"name": "model-id", "required": true, "description": "..."}],
  "flags": [{"name": "page-size", "type": "string", "required": false, "description": "...", "enum": ["10","50"]}]
}
```

### 4. MCP server (`omni mcp serve`) (~250 lines)
New files:
- `internal/mcp/server.go` — MCP server setup and tool registration
- `cmd/omni/mcp_commands.go` — `omni mcp serve` cobra command

**Dependency**: `github.com/mark3labs/mcp-go` (standard Go MCP SDK, stdio transport)

**How it works**:
1. `ParseOperations(specData)` returns all `OperationInfo` structs
2. For each operation, register an MCP tool:
   - **Name**: `{tag-slug}_{command-name}` (e.g., `models_list`, `ai_generate-query`)
   - **Description**: from operation summary/description
   - **Input schema**: JSON Schema built from path params + query params + `body` (string) if `HasBody`
   - **Handler**: resolves config via `config.Resolve()`, builds the URL with path/query params, calls `auth.Do()`, returns JSON response
3. Serve over stdio transport

`cmd/omni/mcp_commands.go`:
- `addMCPCommands(root, specData)` adds `mcp` parent → `serve` child
- Skip auth for `mcp` and `serve` in `PersistentPreRunE`
- Pass config flags (profile, token, org, base-url) through to the MCP tool handlers

Agent configuration example (for AGENTS.md):
```json
{
  "mcpServers": {
    "omni": {
      "command": "omni",
      "args": ["mcp", "serve"],
      "env": { "OMNI_API_KEY": "omni_osk_..." }
    }
  }
}
```

## Implementation Order
1. Export `OperationInfo`/`ParamInfo` + add `ParseOperations()` in `generate.go`
2. `omni openapi` command
3. `--help-json` flag (uses annotations from step 1)
4. MCP server (uses `ParseOperations` from step 1)
5. AGENTS.md (references all new features)
6. Update CLAUDE.md with new commands
7. Update tests

## Files Modified
- `internal/openapi/generate.go` — export types, add `ParseOperations()`, stash annotations
- `cmd/omni/main.go` — `openapi` command, `--help-json` flag, MCP command registration, auth skip list
- `cmd/omni/mcp_commands.go` (new) — `omni mcp serve` cobra command
- `internal/mcp/server.go` (new) — MCP server, tool registration, handlers
- `AGENTS.md` (new) — agent-facing documentation
- `CLAUDE.md` — add new commands to docs
- `go.mod` / `go.sum` — add `mcp-go` dependency

## Verification
1. `make build` succeeds
2. `./bin/omni openapi | jq .info.title` returns the API title
3. `./bin/omni openapi --compact | wc -c` returns compact JSON
4. `./bin/omni models list --help-json` returns structured JSON with method, path, flags
5. `./bin/omni mcp serve` starts and responds to MCP `initialize` + `tools/list` requests (test with `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test"}}}' | ./bin/omni mcp serve`)
6. `make test` passes
