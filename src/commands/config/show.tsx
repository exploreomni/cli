import { Box, render, Text } from 'ink'
import type React from 'react'
import { Table } from '../../components/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import { renderPosix, resolveOutputMode } from '../../output/index.js'
import { executeConfigShow } from './show.execute.js'

const CONFIG_COLUMNS = [
  { key: 'name', header: 'Profile' },
  { key: 'organization', header: 'Organization' },
  { key: 'endpoint', header: 'API Endpoint', width: 35 },
  { key: 'auth', header: 'Auth' },
]

const ConfigShow: React.FC = () => {
  const result = executeConfigShow()

  if (result.data.profiles.length === 0) {
    return (
      <Box flexDirection="column">
        <Text color="yellow">No profiles configured.</Text>
        <Text dimColor>Run `omni-cli config init` to create a profile.</Text>
      </Box>
    )
  }

  const tableData = result.data.profiles.map((p) => ({
    name: p.isDefault ? `${p.name} *` : p.name,
    organization: p.organization,
    endpoint: p.endpoint,
    auth: p.auth,
  }))

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Omni CLI Configuration</Text>
      <Text dimColor>Config file: {result.data.configPath}</Text>
      <Box marginTop={1}>
        <Table columns={CONFIG_COLUMNS} data={tableData} />
      </Box>
      {result.data.profiles.some((p) => p.isDefault) && (
        <Text dimColor>* = default profile</Text>
      )}
    </Box>
  )
}

export const runConfigShow = (options?: { outputMode?: OutputMode }): void => {
  const mode = options?.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(<ConfigShow />)
    return
  }

  const result = executeConfigShow()
  const tableData = result.data.profiles.map((p) => ({
    name: p.isDefault ? `${p.name} *` : p.name,
    organization: p.organization,
    endpoint: p.endpoint,
    auth: p.auth,
  }))

  const tabular: TabularData = {
    columns: CONFIG_COLUMNS,
    rows: tableData,
  }

  renderPosix(mode.format, result.data, tabular)
}
