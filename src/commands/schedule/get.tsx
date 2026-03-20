import { Box, render, Text } from 'ink'
import type React from 'react'
import { useEffect, useState } from 'react'
import { Spinner, StatusMessage } from '../../components/index.js'
import type { OutputMode } from '../../output/index.js'
import {
  renderJson,
  renderPlainTable,
  renderPosixError,
  resolveOutputMode,
} from '../../output/index.js'
import type { ScheduleGetResult } from './get.execute.js'
import { executeScheduleGet } from './get.execute.js'

interface ScheduleGetProps {
  scheduleId: string
  profile?: string
}

const formatStatus = (schedule: ScheduleGetResult['schedule']): string => {
  if (schedule.systemDisabledAt) return 'system-disabled'
  if (schedule.disabledAt) return 'paused'
  return 'active'
}

const ScheduleGet: React.FC<ScheduleGetProps> = ({ scheduleId, profile }) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ScheduleGetResult | null>(null)

  useEffect(() => {
    executeScheduleGet({ scheduleId, profile })
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [scheduleId, profile])

  if (loading) {
    return <Spinner label="Fetching schedule..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result) {
    return <StatusMessage status="error">No data returned</StatusMessage>
  }

  const s = result.schedule
  const status = formatStatus(s)

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>{s.name}</Text>
      <Box flexDirection="column">
        <Text>
          <Text dimColor>ID: </Text>
          {s.id}
        </Text>
        <Text>
          <Text dimColor>Dashboard: </Text>
          {s.dashboardName}
        </Text>
        <Text>
          <Text dimColor>Owner: </Text>
          {s.ownerName}
        </Text>
        <Text>
          <Text dimColor>Destination: </Text>
          {s.destinationType}
        </Text>
        <Text>
          <Text dimColor>Format: </Text>
          {s.format}
        </Text>
        <Text>
          <Text dimColor>Schedule: </Text>
          {s.schedule}
        </Text>
        <Text>
          <Text dimColor>Timezone: </Text>
          {s.timezone}
        </Text>
        <Text>
          <Text dimColor>Status: </Text>
          <Text
            color={
              status === 'active'
                ? 'green'
                : status === 'paused'
                  ? 'yellow'
                  : 'red'
            }
          >
            {status}
          </Text>
        </Text>
        <Text>
          <Text dimColor>Last status: </Text>
          {s.lastStatus ?? '-'}
        </Text>
        <Text>
          <Text dimColor>Last run: </Text>
          {s.lastCompletedAt ?? '-'}
        </Text>
        <Text>
          <Text dimColor>Recipients: </Text>
          {s.recipientCount}
        </Text>
        {s.systemDisabledReason && (
          <Text>
            <Text dimColor>Disabled reason: </Text>
            <Text color="red">{s.systemDisabledReason}</Text>
          </Text>
        )}
        {s.alert && (
          <Box flexDirection="column">
            <Text>
              <Text dimColor>Alert type: </Text>
              {s.alert.conditionType}
            </Text>
            <Text>
              <Text dimColor>Alert query: </Text>
              {s.alert.conditionQueryName ?? '-'}
            </Text>
          </Box>
        )}
      </Box>
    </Box>
  )
}

export const runScheduleGet = (options: {
  scheduleId: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ScheduleGet scheduleId={options.scheduleId} profile={options.profile} />
    )
    return
  }

  executeScheduleGet({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      if (mode.format === 'csv') {
        const s = result.data.schedule
        renderPlainTable({
          columns: [
            { key: 'field', header: 'Field', width: 20 },
            { key: 'value', header: 'Value', width: 50 },
          ],
          rows: [
            { field: 'ID', value: s.id },
            { field: 'Name', value: s.name },
            { field: 'Dashboard', value: s.dashboardName },
            { field: 'Owner', value: s.ownerName },
            { field: 'Destination', value: s.destinationType },
            { field: 'Format', value: s.format },
            { field: 'Schedule', value: s.schedule },
            { field: 'Timezone', value: s.timezone },
            { field: 'Last Status', value: s.lastStatus ?? '-' },
            { field: 'Last Run', value: s.lastCompletedAt ?? '-' },
            { field: 'Recipients', value: String(s.recipientCount) },
          ],
        })
      } else {
        renderJson(result.data.schedule)
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
