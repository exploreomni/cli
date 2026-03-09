import React, { useState, useEffect } from 'react'
import { Box, Text, render } from 'ink'
import { Spinner, StatusMessage, Table } from '../../components/index.js'
import {
  resolveOutputMode,
  renderPosix,
  renderPosixError,
} from '../../output/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import {
  executeScheduleList,
  formatScheduleRow,
} from './list.execute.js'
import type { ScheduleListResult } from './list.execute.js'

interface ScheduleListProps {
  status?: string
  destination?: string
  search?: string
  sort?: string
  sortDirection?: string
  pageSize?: number
  page?: number
  profile?: string
}

const SCHEDULE_COLUMNS = [
  { key: 'name', header: 'Name', width: 25 },
  { key: 'dashboard', header: 'Dashboard', width: 20 },
  { key: 'destination', header: 'Dest', width: 8 },
  { key: 'format', header: 'Format', width: 10 },
  { key: 'status', header: 'Status', width: 16 },
  { key: 'lastStatus', header: 'Last Run', width: 16 },
  { key: 'lastRun', header: 'Last At', width: 12 },
  { key: 'id', header: 'ID' },
]

const statusColors: Record<string, string> = {
  active: 'green',
  paused: 'yellow',
  'system-disabled': 'red',
}

const lastStatusColors: Record<string, string> = {
  COMPLETE: 'green',
  ERROR: 'red',
  ERROR_DELIVERED: 'red',
  KILLED: 'red',
  CONDITION_UNMET: 'yellow',
}

const ScheduleList: React.FC<ScheduleListProps> = (props) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ScheduleListResult | null>(null)

  useEffect(() => {
    executeScheduleList(props)
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <Spinner label="Fetching schedules..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result || result.schedules.length === 0) {
    return (
      <Box flexDirection="column">
        <Text color="yellow">No schedules found.</Text>
      </Box>
    )
  }

  const tableData = result.schedules.map(formatScheduleRow)

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Schedules ({result.totalRecords})</Text>
      <Table columns={SCHEDULE_COLUMNS} data={tableData} />
      {result.hasNextPage && (
        <Text dimColor>More results available. Use --page to paginate.</Text>
      )}
    </Box>
  )
}

export const runScheduleList = (options: {
  status?: string
  destination?: string
  search?: string
  sort?: string
  sortDirection?: string
  pageSize?: number
  page?: number
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(<ScheduleList {...options} />)
    return
  }

  executeScheduleList(options)
    .then((result) => {
      const tabular: TabularData = {
        columns: SCHEDULE_COLUMNS,
        rows: result.data.schedules.map(formatScheduleRow),
      }
      renderPosix(mode.format, result.data.schedules, tabular)
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
