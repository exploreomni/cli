import { Box, Text, useApp, useInput } from 'ink'
import React, { useCallback, useState } from 'react'
import { executeModelValidate } from '../../commands/model/validate.execute.js'
import { Spinner } from '../../components/index.js'
import {
  ActionBar,
  KeyValueFields,
  RetroFrame,
  ToastMessage,
} from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { useRouter } from '../router.js'
import { RETRO } from '../theme.js'

export const ModelDetailView = ({ modelId }: { modelId: string }) => {
  const { pop } = useRouter()
  const { exit } = useApp()
  const { isDetailActive } = usePaneFocus()
  const [validating, setValidating] = useState(false)
  const [showYaml, setShowYaml] = useState(false)
  const [yamlScroll, setYamlScroll] = useState(0)
  const [toast, setToast] = useState<{
    message: string
    type: 'success' | 'error' | 'info'
  } | null>(null)
  const [validationResult, setValidationResult] = useState<{
    valid: boolean
    errorCount: number
    warningCount: number
    infoCount: number
    issues: Array<{ level: string; message: string; path?: string }>
  } | null>(null)

  const { data, loading, error } = useAsyncData(async () => {
    const { executeModelList } = await import(
      '../../commands/model/list.execute.js'
    )
    const result = await executeModelList({})
    const model = result.data.models.find((m) => m.raw.id === modelId)
    return model ?? null
  })

  const {
    data: yamlData,
    loading: yamlLoading,
    error: yamlError,
  } = useAsyncData(async () => {
    const { getConfigManager, getAuthContext, validateAuth } = await import(
      '../../config/index.js'
    )
    const { createAPIClient, getModelYaml } = await import('../../api/index.js')

    const configManager = getConfigManager()
    const profileData = configManager.getProfile()
    if (!profileData) return null

    const authContext = getAuthContext()
    const authError = validateAuth(authContext)
    if (authError) return null

    const client = createAPIClient(profileData.apiEndpoint, authContext)
    const result = await getModelYaml(client, modelId)
    if (result.error || !result.data) return null

    return { ...result.data, baseUrl: profileData.apiEndpoint }
  })

  const handleValidate = useCallback(async () => {
    setValidating(true)
    try {
      const result = await executeModelValidate({ modelId })
      setValidationResult(result.data)
      setToast({
        message: result.data.valid ? 'Model is valid' : 'Validation failed',
        type: result.data.valid ? 'success' : 'error',
      })
    } catch (err: unknown) {
      setToast({
        message: `Validation error: ${err instanceof Error ? err.message : String(err)}`,
        type: 'error',
      })
    } finally {
      setValidating(false)
    }
  }, [modelId])

  const yamlLines = React.useMemo(() => {
    if (!yamlData?.files) return []
    return Object.entries(yamlData.files).flatMap(([filename, content]) => [
      `--- ${filename}`,
      ...content.split('\n'),
    ])
  }, [yamlData])

  const VISIBLE_YAML_LINES = 20

  useInput(
    (input, key) => {
      if (key.escape) {
        if (showYaml) {
          setShowYaml(false)
          setYamlScroll(0)
          return
        }
        pop()
        return
      }
      if (input === 'q') {
        exit()
        return
      }
      if (input === 'y') {
        setShowYaml((prev) => !prev)
        setYamlScroll(0)
        return
      }
      if (input === 'v' && !validating && !showYaml) handleValidate()

      if (showYaml) {
        if (key.downArrow || input === 'j') {
          setYamlScroll((prev) =>
            Math.min(
              prev + 1,
              Math.max(0, yamlLines.length - VISIBLE_YAML_LINES)
            )
          )
        }
        if (key.upArrow || input === 'k') {
          setYamlScroll((prev) => Math.max(0, prev - 1))
        }
      }
    },
    { isActive: isDetailActive }
  )

  if (loading) {
    return (
      <RetroFrame title="MODEL DETAIL" borderless>
        <Spinner label="Loading..." />
      </RetroFrame>
    )
  }

  if (error) {
    return (
      <RetroFrame title="MODEL DETAIL" borderless>
        <Text color={RETRO.colors.error}>Error: {error}</Text>
      </RetroFrame>
    )
  }

  if (!data) {
    return (
      <RetroFrame title="MODEL DETAIL" borderless>
        <Text color={RETRO.colors.error}>Model not found</Text>
      </RetroFrame>
    )
  }

  const baseUrl = yamlData?.baseUrl?.replace(/\/$/, '') ?? ''
  const modelEditorUrl = baseUrl
    ? `${baseUrl}/models/${data.raw.id}/ide`
    : `/models/${data.raw.id}/ide`

  const fields: [string, string][] = [
    ['Name', data.name],
    ['Kind', data.kind],
    ['Updated', data.updated],
    ['ID', data.raw.id],
  ]

  if (data.raw.connectionId) {
    fields.push(['Connection', data.raw.connectionId])
  }

  fields.push(['Editor', modelEditorUrl])

  const actions = [
    { key: 'y', label: showYaml ? 'Hide YAML' : 'Show YAML' },
    ...(showYaml ? [] : [{ key: 'v', label: 'Validate' }]),
    { key: 'Esc', label: showYaml ? 'Close YAML' : 'Back' },
    { key: 'q', label: 'Quit' },
  ]

  return (
    <Box flexDirection="column">
      <RetroFrame
        title={`MODEL: ${data.name}`}
        borderless
        footer={<ActionBar actions={actions} />}
      >
        <Box flexDirection="column">
          <KeyValueFields fields={fields} />

          {showYaml && (
            <Box flexDirection="column" marginTop={1}>
              <Text color={RETRO.colors.highlight} bold>
                YAML{' '}
                <Text color={RETRO.colors.dim} bold={false}>
                  (j/k or arrows to scroll)
                </Text>
              </Text>
              <Box
                flexDirection="column"
                borderStyle="single"
                borderColor={RETRO.colors.dim}
                paddingX={1}
                marginTop={1}
              >
                {yamlLoading && <Spinner label="Loading YAML..." />}
                {yamlError && (
                  <Text color={RETRO.colors.error}>
                    Failed to load YAML: {yamlError}
                  </Text>
                )}
                {!yamlLoading && !yamlError && yamlLines.length === 0 && (
                  <Text color={RETRO.colors.dim}>No YAML files found</Text>
                )}
                {!yamlLoading &&
                  yamlLines
                    .slice(yamlScroll, yamlScroll + VISIBLE_YAML_LINES)
                    .map((line, i) => (
                      <Text
                        key={yamlScroll + i}
                        color={
                          line.startsWith('---')
                            ? RETRO.colors.highlight
                            : RETRO.colors.primary
                        }
                      >
                        {line}
                      </Text>
                    ))}
                {yamlLines.length > VISIBLE_YAML_LINES && (
                  <Text color={RETRO.colors.dim}>
                    [{yamlScroll + 1}-
                    {Math.min(
                      yamlScroll + VISIBLE_YAML_LINES,
                      yamlLines.length
                    )}{' '}
                    of {yamlLines.length} lines]
                  </Text>
                )}
              </Box>
            </Box>
          )}

          {validating && (
            <Box marginTop={1}>
              <Spinner label="Validating..." />
            </Box>
          )}

          {validationResult && !showYaml && (
            <Box flexDirection="column" marginTop={1}>
              <Text
                color={
                  validationResult.valid
                    ? RETRO.colors.success
                    : RETRO.colors.error
                }
                bold
              >
                {validationResult.valid ? 'VALID' : 'INVALID'}
              </Text>
              <Text color={RETRO.colors.dim}>
                {validationResult.errorCount} errors,{' '}
                {validationResult.warningCount} warnings,{' '}
                {validationResult.infoCount} info
              </Text>
              {validationResult.issues.slice(0, 10).map((issue, i) => (
                <Box key={i} gap={1}>
                  <Text
                    color={
                      issue.level === 'error'
                        ? RETRO.colors.error
                        : issue.level === 'warning'
                          ? RETRO.colors.warning
                          : RETRO.colors.dim
                    }
                  >
                    [{issue.level}]
                  </Text>
                  <Text color={RETRO.colors.primary}>{issue.message}</Text>
                  {issue.path && (
                    <Text color={RETRO.colors.dim}>({issue.path})</Text>
                  )}
                </Box>
              ))}
              {validationResult.issues.length > 10 && (
                <Text color={RETRO.colors.dim}>
                  ... and {validationResult.issues.length - 10} more
                </Text>
              )}
            </Box>
          )}
        </Box>
      </RetroFrame>

      {toast && (
        <ToastMessage
          message={toast.message}
          type={toast.type}
          onDismiss={() => setToast(null)}
        />
      )}
    </Box>
  )
}
