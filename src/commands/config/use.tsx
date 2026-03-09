import React from 'react'
import { Box, render } from 'ink'
import { getConfigManager } from '../../config/index.js'
import { StatusMessage } from '../../components/index.js'
import {
  resolveOutputMode,
  renderPosixError,
  renderPosixSuccess,
  renderJson,
} from '../../output/index.js'
import type { OutputMode } from '../../output/index.js'
import { executeConfigUse } from './use.execute.js'

interface ConfigUseProps {
  profileName: string
}

const ConfigUse: React.FC<ConfigUseProps> = ({ profileName }) => {
  const configManager = getConfigManager()
  const profile = configManager.getProfile(profileName)

  if (!profile) {
    const available = Object.keys(configManager.getProfiles())
    return (
      <Box flexDirection="column">
        <StatusMessage status="error">
          Profile &apos;{profileName}&apos; not found
        </StatusMessage>
        {available.length > 0 && (
          <Box marginTop={1} flexDirection="column">
            <StatusMessage status="info">Available profiles:</StatusMessage>
            {available.map((name) => (
              <Box key={name} marginLeft={2}>
                <StatusMessage status="info">- {name}</StatusMessage>
              </Box>
            ))}
          </Box>
        )}
      </Box>
    )
  }

  configManager.setDefaultProfile(profileName)

  return (
    <StatusMessage status="success">
      Switched to profile &apos;{profileName}&apos;
    </StatusMessage>
  )
}

export const runConfigUse = (
  profileName: string,
  options?: { outputMode?: OutputMode }
): void => {
  const mode = options?.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(<ConfigUse profileName={profileName} />)
    return
  }

  try {
    const result = executeConfigUse(profileName)
    if (mode.format === 'json') {
      renderJson(result.data)
    } else {
      renderPosixSuccess(`Switched to profile '${result.data.switchedTo}'`)
    }
  } catch (e) {
    renderPosixError((e as Error).message)
    process.exit(1)
  }
}
