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
import { executeScheduleTrigger } from './trigger.execute.js'

interface ScheduleTriggerProps {
  scheduleId: string
  profile?: string
}

const ScheduleTrigger: React.FC<ScheduleTriggerProps> = ({
  scheduleId,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  useEffect(() => {
    executeScheduleTrigger({ scheduleId, profile })
      .then(() => setSuccess(true))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [scheduleId, profile])

  if (loading) {
    return <Spinner label="Triggering schedule..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  return (
    <StatusMessage status="success">
      Schedule {scheduleId.slice(0, 8)} triggered
    </StatusMessage>
  )
}

export const runScheduleTrigger = (options: {
  scheduleId: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ScheduleTrigger
        scheduleId={options.scheduleId}
        profile={options.profile}
      />
    )
    return
  }

  executeScheduleTrigger({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      if (mode.format === 'json') {
        renderJson(result.data)
      } else {
        renderPosixSuccess(
          `Schedule ${options.scheduleId.slice(0, 8)} triggered`
        )
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
