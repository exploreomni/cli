import { getConfigManager } from '../../config/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ConfigUseResult {
  switchedTo: string
}

export const executeConfigUse = (
  profileName: string
): CommandResult<ConfigUseResult> => {
  const configManager = getConfigManager()
  const profile = configManager.getProfile(profileName)

  if (!profile) {
    const available = Object.keys(configManager.getProfiles())
    const msg =
      available.length > 0
        ? `Profile '${profileName}' not found. Available: ${available.join(', ')}`
        : `Profile '${profileName}' not found. No profiles configured.`
    throw new Error(msg)
  }

  configManager.setDefaultProfile(profileName)

  return {
    data: { switchedTo: profileName },
  }
}
