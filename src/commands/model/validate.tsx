import React, { useState, useEffect } from 'react'
import { Box, Text, render } from 'ink'
import { Spinner, StatusMessage } from '../../components/index.js'
import {
  resolveOutputMode,
  renderPosix,
  renderPosixError,
} from '../../output/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import { executeModelValidate } from './validate.execute.js'
import type { ModelValidateResult } from './validate.execute.js'

interface ModelValidateProps {
  modelId: string
  branchId?: string
  profile?: string
}

const levelColors: Record<string, string> = {
  error: 'red',
  warning: 'yellow',
  info: 'blue',
}

const levelSymbols: Record<string, string> = {
  error: '✗',
  warning: '⚠',
  info: 'ℹ',
}

const ISSUE_COLUMNS = [
  { key: 'level', header: 'Level', width: 10 },
  { key: 'path', header: 'Path', width: 30 },
  { key: 'message', header: 'Message', width: 50 },
]

const ModelValidate: React.FC<ModelValidateProps> = ({
  modelId,
  branchId,
  profile,
}) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ModelValidateResult | null>(null)

  useEffect(() => {
    executeModelValidate({ modelId, branchId, profile })
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [modelId, branchId, profile])

  if (loading) {
    return <Spinner label="Validating model..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result) {
    return <StatusMessage status="error">No data returned</StatusMessage>
  }

  return (
    <Box flexDirection="column" gap={1}>
      <Box>
        {result.valid ? (
          <StatusMessage status="success">Model is valid</StatusMessage>
        ) : (
          <StatusMessage status="error">
            Model has validation issues
          </StatusMessage>
        )}
      </Box>

      {result.issues.length > 0 && (
        <Box flexDirection="column">
          <Text dimColor>
            {result.errorCount} error{result.errorCount !== 1 ? 's' : ''},{' '}
            {result.warningCount} warning
            {result.warningCount !== 1 ? 's' : ''}, {result.infoCount} info
          </Text>
          <Box marginTop={1} flexDirection="column">
            {result.issues.map((issue, idx) => (
              <Box key={idx}>
                <Text color={levelColors[issue.level]}>
                  {levelSymbols[issue.level]}
                </Text>
                <Text> </Text>
                {issue.path && <Text dimColor>[{issue.path}] </Text>}
                <Text>{issue.message}</Text>
              </Box>
            ))}
          </Box>
        </Box>
      )}

      {result.issues.length === 0 && result.valid && (
        <Text dimColor>No issues found</Text>
      )}
    </Box>
  )
}

export const runModelValidate = (options: {
  modelId: string
  branchId?: string
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(
      <ModelValidate
        modelId={options.modelId}
        branchId={options.branchId}
        profile={options.profile}
      />
    )
    return
  }

  executeModelValidate({
    modelId: options.modelId,
    branchId: options.branchId,
    profile: options.profile,
  })
    .then((result) => {
      const tabular: TabularData = {
        columns: ISSUE_COLUMNS,
        rows: result.data.issues.map((i) => ({
          level: i.level,
          path: i.path ?? '',
          message: i.message,
        })),
      }
      renderPosix(
        mode.format,
        {
          valid: result.data.valid,
          issues: result.data.issues,
          errorCount: result.data.errorCount,
          warningCount: result.data.warningCount,
          infoCount: result.data.infoCount,
        },
        tabular
      )
      if (result.exitCode) {
        process.exit(result.exitCode)
      }
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
