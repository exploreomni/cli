import { Box, render, Text } from 'ink'
import type React from 'react'
import { useEffect, useState } from 'react'
import { Spinner, StatusMessage, Table } from '../../components/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import {
  renderPosix,
  renderPosixError,
  resolveOutputMode,
} from '../../output/index.js'
import type { ScheduleRecipientsResult } from './recipients.execute.js'
import { executeScheduleRecipients } from './recipients.execute.js'

interface ScheduleRecipientsProps {
  scheduleId: string
  profile?: string
}

const RECIPIENT_COLUMNS = [
  { key: 'name', header: 'Name', width: 25 },
  { key: 'email', header: 'Email', width: 30 },
  { key: 'group', header: 'Group', width: 20 },
]

const ScheduleRecipients: React.FC<ScheduleRecipientsProps> = ({
  scheduleId,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ScheduleRecipientsResult | null>(null)

  useEffect(() => {
    executeScheduleRecipients({ scheduleId, profile })
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [scheduleId, profile])

  if (loading) {
    return <Spinner label="Fetching recipients..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result || result.recipients.length === 0) {
    return (
      <Box flexDirection="column">
        <Text dimColor>Destination type: {result?.type ?? 'unknown'}</Text>
        <Text color="yellow">No recipients found.</Text>
      </Box>
    )
  }

  const tableData = result.recipients.map((r) => ({
    name: r.name,
    email: r.email,
    group: r.group ?? '-',
  }))

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Recipients ({result.recipients.length})</Text>
      <Text dimColor>Destination type: {result.type}</Text>
      <Table columns={RECIPIENT_COLUMNS} data={tableData} />
    </Box>
  )
}

export const runScheduleRecipients = (options: {
  scheduleId: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ScheduleRecipients
        scheduleId={options.scheduleId}
        profile={options.profile}
      />
    )
    return
  }

  executeScheduleRecipients({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      const tabular: TabularData = {
        columns: RECIPIENT_COLUMNS,
        rows: result.data.recipients.map((r) => ({
          name: r.name,
          email: r.email,
          group: r.group ?? '-',
        })),
      }
      renderPosix(mode.format, result.data.recipients, tabular)
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
