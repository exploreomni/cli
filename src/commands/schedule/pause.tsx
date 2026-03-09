import React, { useState, useEffect } from 'react'
import { render } from 'ink'
import { Spinner, StatusMessage } from '../../components/index.js'
import {
  resolveOutputMode,
  renderJson,
  renderPosixError,
  renderPosixSuccess,
} from '../../output/index.js'
import type { OutputMode } from '../../output/index.js'
import { executeSchedulePause } from './pause.execute.js'

interface SchedulePauseProps {
  scheduleId: string
  profile?: string
}

const SchedulePause: React.FC<SchedulePauseProps> = ({
  scheduleId,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  useEffect(() => {
    executeSchedulePause({ scheduleId, profile })
      .then(() => setSuccess(true))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [scheduleId, profile])

  if (loading) {
    return <Spinner label="Pausing schedule..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  return (
    <StatusMessage status="success">
      Schedule {scheduleId.slice(0, 8)} paused
    </StatusMessage>
  )
}

export const runSchedulePause = (options: {
  scheduleId: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <SchedulePause
        scheduleId={options.scheduleId}
        profile={options.profile}
      />
    )
    return
  }

  executeSchedulePause({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      if (mode.format === 'json') {
        renderJson(result.data)
      } else {
        renderPosixSuccess(
          `Schedule ${options.scheduleId.slice(0, 8)} paused`
        )
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
