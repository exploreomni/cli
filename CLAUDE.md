# omni-cli

CLI and interactive TUI for Omni. Uses [Cleye](https://github.com/privatenumber/cleye) for command parsing and [Ink](https://github.com/vadimdemedes/ink) (React for terminals) for the TUI.

## OpenAPI Spec

The backend returns **snake_case** JSON (Jackson serialization from Kotlin). The CLI's Zod schemas in `src/api/types.ts` use **camelCase** because the Remix API layer converts keys before responding.

## Project Structure

```
bin/omni.ts              # Entry point, command registration (Cleye)
src/
  api/                   # API client + endpoint wrappers
    client.ts            # APIClient class (GET/POST/PUT/DELETE)
    types.ts             # Zod schemas for models, validation, pagination
    schedule-types.ts    # Schedule-specific Zod schemas
    models.ts            # Model API functions (listModels, validateModel, getModelYaml)
    schedules.ts         # Schedule API functions
    query.ts             # AI query generation API
  commands/              # CLI command implementations
    config/              # config:init, config:show, config:use
    model/               # model:list, model:validate
    query/               # query (AI-generated queries)
    schedule/            # schedule:list, schedule:get, schedule:trigger, etc.
  config/                # Profile management, auth
    config-manager.ts    # Profile storage (uses `conf` package)
    auth.ts              # Auth context, headers, validation
    schema.ts            # Config Zod schemas (Profile, Preferences)
  output/                # Output formatting
    mode.ts              # Resolve TUI vs JSON/CSV/table
    posix.ts             # Render JSON, CSV, plain table
    flags.ts             # Shared --format, --no-tui, --no-color flags
  tui/                   # Interactive terminal UI (Ink/React)
    index.tsx            # TUI entry point (runTui)
    router.tsx           # Stack-based navigation (push/pop)
    theme.ts             # Colors, borders, symbols
    views/               # Full-screen views
    components/          # RetroFrame, SelectableList, ActionBar, ConfirmDialog, ToastMessage
    hooks/               # useAsyncData, useBulkAction
```

## Adding a CLI Command

Each command has two files: an execute file (business logic) and a tsx file (rendering).

### 1. Create the execute file

`src/commands/feature/name.execute.ts` — pure async logic, no React:

```typescript
import { getConfigManager, getAuthContext, validateAuth } from '../../config/index.js'
import { createAPIClient } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface FeatureNameResult {
  items: Array<{ id: string; name: string }>
}

export const executeFeatureName = async (options: {
  profile?: string
}): Promise<CommandResult<FeatureNameResult>> => {
  const configManager = getConfigManager()
  const profileData = configManager.getProfile(options.profile)
  if (!profileData) throw new Error('No profile configured. Run `omni-cli config:init` first.')

  const authContext = getAuthContext(options.profile)
  const authError = validateAuth(authContext)
  if (authError) throw new Error(authError)

  const client = createAPIClient(profileData.apiEndpoint, authContext)
  const result = await client.get<SomeResponse>('/api/v1/endpoint')

  if (result.error) throw new Error(result.error)
  if (!result.data) throw new Error('No data returned')

  return { data: { items: result.data.records }, exitCode: 0 }
}
```

### 2. Create the tsx file

`src/commands/feature/name.tsx` — React component + `run*` entry point:

```typescript
import React, { useState, useEffect } from 'react'
import { render, Box, Text } from 'ink'
import { Spinner } from '../../components/index.js'
import { resolveOutputMode, renderPosix, renderPosixError } from '../../output/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import { executeFeatureName } from './name.execute.js'

const FeatureName = ({ profile }: { profile?: string }) => {
  const [loading, setLoading] = useState(true)
  const [data, setData] = useState<FeatureNameResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    executeFeatureName({ profile })
      .then((r) => setData(r.data))
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <Spinner label="Loading..." />
  if (error) return <Text color="red">Error: {error}</Text>
  // ... render with Ink
}

export const runFeatureName = (options: {
  profile?: string
  outputMode?: OutputMode
}) => {
  const mode = options.outputMode ?? resolveOutputMode({})
  if (mode.isTUI) {
    render(<FeatureName profile={options.profile} />)
  } else {
    executeFeatureName(options)
      .then((result) => {
        const tabular: TabularData = {
          columns: [{ key: 'name', header: 'Name' }],
          rows: result.data.items,
        }
        renderPosix(mode.format, result.data, tabular)
      })
      .catch((e) => { renderPosixError(e.message); process.exit(1) })
  }
}
```

### 3. Export and register

1. Create `src/commands/feature/index.ts` exporting `runFeatureName`
2. Add to `src/commands/index.ts`
3. Register in `bin/omni.ts`:

```typescript
command(
  {
    name: 'feature:name',
    alias: 'fn',
    flags: { profile: { type: String, alias: 'p' }, ...outputFlags },
    help: { description: 'Do the thing' },
  },
  (argv) => runFeatureName({
    profile: argv.flags.profile,
    outputMode: resolveOutputMode({
      format: argv.flags.format,
      noTui: argv.flags['no-tui'],
      noColor: argv.flags['no-color'],
    }),
  })
),
```

## Adding a TUI View

### 1. Add the route type

In `src/tui/router.tsx`, add to the `Route` union:

```typescript
| { view: 'feature-detail'; featureId: string }
```

### 2. Create the view

`src/tui/views/FeatureDetailView.tsx`:

```typescript
import React from 'react'
import { Box, Text, useApp, useInput } from 'ink'
import { RetroFrame, ActionBar } from '../components/index.js'
import { useRouter } from '../router.js'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { RETRO } from '../theme.js'
import { Spinner } from '../../components/index.js'

export const FeatureDetailView = ({ featureId }: { featureId: string }) => {
  const { pop } = useRouter()
  const { exit } = useApp()

  const { data, loading, error } = useAsyncData(async () => {
    // fetch data
  })

  useInput((input, key) => {
    if (key.escape) { pop(); return }
    if (input === 'q') { exit(); return }
  })

  if (loading) return <RetroFrame title="FEATURE"><Spinner label="Loading..." /></RetroFrame>
  if (error) return <RetroFrame title="FEATURE"><Text color={RETRO.colors.error}>Error: {error}</Text></RetroFrame>

  return (
    <RetroFrame
      title="FEATURE"
      footer={<ActionBar actions={[{ key: 'Esc', label: 'Back' }, { key: 'q', label: 'Quit' }]} />}
    >
      {/* content */}
    </RetroFrame>
  )
}
```

### 3. Wire it up

1. Export from `src/tui/views/index.ts`
2. Add case to `ViewRouter` in `src/tui/index.tsx`
3. Navigate to it with `push({ view: 'feature-detail', featureId: '...' })`

## Adding an API Endpoint Wrapper

### 1. Define Zod schemas in `src/api/types.ts`

```typescript
export const FeatureResponseSchema = z.object({
  id: z.string(),
  name: z.string(),
})
export type FeatureResponse = z.infer<typeof FeatureResponseSchema>
```

### 2. Add the wrapper function in the appropriate file under `src/api/`

```typescript
export const getFeature = async (
  client: APIClient,
  featureId: string
): Promise<APIResponse<FeatureResponse>> => {
  return client.get<FeatureResponse>(`/api/v1/features/${featureId}`)
}
```

### 3. Export from `src/api/index.ts`

## TUI Components

- **RetroFrame** — bordered container with title and optional footer
- **SelectableList** — cursor-navigable list with optional multi-select
- **ActionBar** — displays keyboard shortcuts
- **ConfirmDialog** — yes/no prompt
- **ToastMessage** — temporary notification

## TUI Hooks

- **useAsyncData(fn)** — returns `{ data, loading, error, reload }`
- **useBulkAction(fn, onComplete)** — execute an action across multiple items

## Development

```bash
npx tsx ./bin/omni.ts <command>   # Run the CLI
npm run lint:ts                   # Type check
npm test                          # Run tests
```
