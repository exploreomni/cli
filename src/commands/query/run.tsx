import React, { useState, useEffect } from 'react'
import { Box, Text, render } from 'ink'
import { Spinner, StatusMessage, Table } from '../../components/index.js'
import {
  resolveOutputMode,
  renderPosix,
  renderPosixError,
} from '../../output/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import { executeQueryRun } from './run.execute.js'
import type { QueryRunResult } from './run.execute.js'

interface QueryRunProps {
  prompt: string
  modelId: string
  topic?: string
  profile?: string
}

const formatValue = (value: unknown): string => {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'number') {
    return value.toLocaleString()
  }
  return String(value)
}

const QueryRun: React.FC<QueryRunProps> = ({
  prompt,
  modelId,
  topic,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<QueryRunResult | null>(null)

  useEffect(() => {
    executeQueryRun({ prompt, modelId, topic, profile })
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [prompt, modelId, topic, profile])

  if (loading) {
    return <Spinner label="Generating and running query..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result || result.rows.length === 0) {
    return (
      <Box flexDirection="column">
        <StatusMessage status="success">Query generated</StatusMessage>
        {result?.topic && <Text dimColor>Topic: {result.topic}</Text>}
        <Text dimColor>No results returned</Text>
      </Box>
    )
  }

  const tableData = result.rows.slice(0, 50).map((row) => {
    const obj: Record<string, string> = {}
    result.columns.forEach((col, i) => {
      obj[col] = formatValue(row[i])
    })
    return obj
  })

  const columns = result.columns.map((col) => ({
    key: col,
    header: col,
    width: Math.min(20, Math.max(col.length, 10)),
  }))

  return (
    <Box flexDirection="column" gap={1}>
      <Box>
        <StatusMessage status="success">Query executed</StatusMessage>
        {result.topic && <Text dimColor> (topic: {result.topic})</Text>}
      </Box>
      <Text dimColor>
        {result.rowCount} row{result.rowCount !== 1 ? 's' : ''}
        {result.rowCount > 50 ? ' (showing first 50)' : ''}
      </Text>
      <Table columns={columns} data={tableData} />
    </Box>
  )
}

export const runQueryRun = (options: {
  prompt: string
  modelId: string
  topic?: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <QueryRun
        prompt={options.prompt}
        modelId={options.modelId}
        topic={options.topic}
        profile={options.profile}
      />
    )
    return
  }

  executeQueryRun({
    prompt: options.prompt,
    modelId: options.modelId,
    topic: options.topic,
    profile: options.profile,
  })
    .then((result) => {
      const jsonData = result.data.rows.map((row) => {
        const obj: Record<string, unknown> = {}
        result.data.columns.forEach((col, i) => {
          obj[col] = row[i]
        })
        return obj
      })

      const tabular: TabularData = {
        columns: result.data.columns.map((col) => ({
          key: col,
          header: col,
          width: Math.min(20, Math.max(col.length, 10)),
        })),
        rows: jsonData.map((obj) => {
          const row: Record<string, unknown> = {}
          for (const [k, v] of Object.entries(obj)) {
            row[k] = formatValue(v)
          }
          return row
        }),
      }

      renderPosix(mode.format, jsonData, tabular)
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
