import React, { useState, useEffect } from 'react'
import { Box, Text, render, useInput, useApp } from 'ink'
import { Spinner, StatusMessage } from '../../components/index.js'
import {
  resolveOutputMode,
  renderJson,
  renderPosixError,
  renderPosixSuccess,
} from '../../output/index.js'
import type { OutputMode } from '../../output/index.js'
import { executeScheduleDelete } from './delete.execute.js'

interface ScheduleDeleteProps {
  scheduleId: string
  profile?: string
  force?: boolean
}

const ScheduleDelete: React.FC<ScheduleDeleteProps> = ({
  scheduleId,
  profile,
  force,
}) => {
  const { exit } = useApp()
  const [confirming, setConfirming] = useState(!force)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  useInput((input) => {
    if (!confirming) return

    if (input.toLowerCase() === 'y') {
      setConfirming(false)
      setLoading(true)
      executeScheduleDelete({ scheduleId, profile })
        .then(() => setSuccess(true))
        .catch((e: Error) => setError(e.message))
        .finally(() => setLoading(false))
    } else if (input.toLowerCase() === 'n') {
      exit()
    }
  })

  useEffect(() => {
    if (force) {
      setLoading(true)
      executeScheduleDelete({ scheduleId, profile })
        .then(() => setSuccess(true))
        .catch((e: Error) => setError(e.message))
        .finally(() => setLoading(false))
    }
  }, [scheduleId, profile, force])

  if (confirming) {
    return (
      <Box flexDirection="column">
        <Text>
          Delete schedule {scheduleId.slice(0, 8)}? This cannot be undone.
        </Text>
        <Text dimColor>Press y to confirm, n to cancel</Text>
      </Box>
    )
  }

  if (loading) {
    return <Spinner label="Deleting schedule..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  return (
    <StatusMessage status="success">
      Schedule {scheduleId.slice(0, 8)} deleted
    </StatusMessage>
  )
}

export const runScheduleDelete = (options: {
  scheduleId: string
  force?: boolean
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ScheduleDelete
        scheduleId={options.scheduleId}
        profile={options.profile}
        force={options.force}
      />
    )
    return
  }

  // In POSIX mode, skip confirmation (scripts should use --force for safety)
  executeScheduleDelete({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      if (mode.format === 'json') {
        renderJson(result.data)
      } else {
        renderPosixSuccess(
          `Schedule ${options.scheduleId.slice(0, 8)} deleted`
        )
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
