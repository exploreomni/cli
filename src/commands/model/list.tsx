import React, { useState, useEffect } from 'react'
import { Box, Text, render } from 'ink'
import { Spinner, StatusMessage, Table } from '../../components/index.js'
import {
  resolveOutputMode,
  renderPosix,
  renderPosixError,
} from '../../output/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import { executeModelList } from './list.execute.js'
import type { ModelListResult } from './list.execute.js'

interface ModelListProps {
  modelKind?: string
  profile?: string
}

const MODEL_COLUMNS = [
  { key: 'name', header: 'Name', width: 30 },
  { key: 'kind', header: 'Kind', width: 18 },
  { key: 'updated', header: 'Updated', width: 12 },
  { key: 'id', header: 'ID' },
]

const ModelList: React.FC<ModelListProps> = ({ modelKind, profile }) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ModelListResult | null>(null)

  useEffect(() => {
    executeModelList({ modelKind, profile })
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [modelKind, profile])

  if (loading) {
    return <Spinner label="Fetching models..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result || result.models.length === 0) {
    return (
      <Box flexDirection="column">
        <Text color="yellow">No models found.</Text>
        {modelKind && (
          <Text dimColor>Filter: modelKind={modelKind}</Text>
        )}
      </Box>
    )
  }

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Models ({result.count})</Text>
      <Table columns={MODEL_COLUMNS} data={result.models} />
    </Box>
  )
}

export const runModelList = (options: {
  modelKind?: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(<ModelList modelKind={options.modelKind} profile={options.profile} />)
    return
  }

  executeModelList({ modelKind: options.modelKind, profile: options.profile })
    .then((result) => {
      const tabular: TabularData = {
        columns: MODEL_COLUMNS,
        rows: result.data.models,
      }
      renderPosix(mode.format, result.data.models.map((m) => m.raw), tabular)
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
