import { render } from 'ink'
import type React from 'react'
import { useEffect, useState } from 'react'
import { Spinner, StatusMessage } from '../../components/index.js'
import type { OutputMode } from '../../output/index.js'
import {
  renderJson,
  renderPosixError,
  renderPosixSuccess,
  resolveOutputMode,
} from '../../output/index.js'
import { executeScheduleResume } from './resume.execute.js'

interface ScheduleResumeProps {
  scheduleId: string
  profile?: string
}

const ScheduleResume: React.FC<ScheduleResumeProps> = ({
  scheduleId,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [_success, setSuccess] = useState(false)

  useEffect(() => {
    executeScheduleResume({ scheduleId, profile })
      .then(() => setSuccess(true))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [scheduleId, profile])

  if (loading) {
    return <Spinner label="Resuming schedule..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  return (
    <StatusMessage status="success">
      Schedule {scheduleId.slice(0, 8)} resumed
    </StatusMessage>
  )
}

export const runScheduleResume = (options: {
  scheduleId: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ScheduleResume
        scheduleId={options.scheduleId}
        profile={options.profile}
      />
    )
    return
  }

  executeScheduleResume({
    scheduleId: options.scheduleId,
    profile: options.profile,
  })
    .then((result) => {
      if (mode.format === 'json') {
        renderJson(result.data)
      } else {
        renderPosixSuccess(`Schedule ${options.scheduleId.slice(0, 8)} resumed`)
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
