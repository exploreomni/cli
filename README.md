# Omni CLI

Command-line tool for managing Omni resources — models, queries, and schedules.

## Getting started

### 1. Install dependencies

```bash
npm install
```

### 2. Create a profile

```bash
npx tsx ./bin/omni.ts config:init
```

This walks you through setting up your organization, API endpoint, and authentication. You can create multiple profiles for different orgs or environments.

### 3. Set your API key

Either enter it during `config:init`, or set the environment variable:

```bash
export OMNI_API_KEY=omni_osk_...
```

### 4. Run a command

```bash
npx tsx ./bin/omni.ts model:list
```

## Commands

### Config

| Command | Alias | Description |
|---------|-------|-------------|
| `config:init` | `ci` | Create a new profile (interactive wizard) |
| `config:show` | `cs` | Display all configured profiles |
| `config:use <profile>` | `cu` | Switch the active profile |

### Models

| Command | Alias | Description |
|---------|-------|-------------|
| `model:list` | `ml` | List models in the organization |
| `model:validate <modelId>` | `mv` | Validate a model's schema |

`model:list` flags: `--kind` (schema, shared, branch), `--profile`

`model:validate` flags: `--branch`, `--profile`

### Query

| Command | Alias | Description |
|---------|-------|-------------|
| `query <prompt>` | `q` | Generate and run an AI query from natural language |

Requires `--model` (`-m`). Optional: `--topic` (`-t`), `--profile` (`-p`).

```bash
npx tsx ./bin/omni.ts query "top 10 customers by revenue" -m <modelId>
```

### Schedules

| Command | Alias | Description |
|---------|-------|-------------|
| `schedule:list` | `sl` | List schedules with filtering and pagination |
| `schedule:get <id>` | `sg` | Show schedule details |
| `schedule:trigger <id>` | `st` | Trigger a schedule immediately |
| `schedule:pause <id>` | `sp` | Pause a schedule |
| `schedule:resume <id>` | `sr` | Resume a paused schedule |
| `schedule:delete <id>` | `sd` | Delete a schedule |
| `schedule:recipients <id>` | | List recipients for a schedule |

`schedule:list` flags: `--status`, `--destination`, `--search`, `--sort`, `--sort-direction`, `--page-size`, `--page`

### TUI

```bash
npx tsx ./bin/omni.ts tui
```

Launch the interactive terminal UI for browsing models, schedules, users, and configuration.

## Output modes

By default the CLI renders an interactive TUI with colors and spinners when running in a terminal. When piped or redirected, it automatically switches to JSON.

You can control this explicitly:

```bash
# JSON output
omni-cli model:list --format json
omni-cli schedule:list -F json | jq '.[].name'

# CSV output
omni-cli model:list --format csv > models.csv

# Plain table (no colors or spinners)
omni-cli model:list --no-tui

# Disable color (also respects NO_COLOR env var)
omni-cli model:list --no-color
```

The `--format` (`-F`), `--no-tui`, and `--no-color` flags are available on all commands except `config:init`.

## Environment variables

| Variable | Description |
|----------|-------------|
| `OMNI_API_KEY` | API key for authentication |
| `OMNI_API_TOKEN` | Bearer token (alternative to API key) |
| `OMNI_ORG_ID` | Override the organization ID from your profile |
| `NO_COLOR` | Disable colored output (standard convention) |

## Development

```bash
npx tsx ./bin/omni.ts <command>   # Run the CLI
npm run lint:ts                   # Type check
npm test                          # Run tests
npm run test:watch                # Run tests in watch mode
```
