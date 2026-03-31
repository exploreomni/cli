import { getConfigManager } from '../../config/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ConfigShowResult {
  profiles: Array<{
    name: string
    isDefault: boolean
    organization: string
    endpoint: string
    auth: string
  }>
  configPath: string
}

export const executeConfigShow = (): CommandResult<ConfigShowResult> => {
  const configManager = getConfigManager()
  const defaultProfile = configManager.getDefaultProfile()
  const profiles = configManager.getProfiles()
  const profileNames = Object.keys(profiles)

  return {
    data: {
      profiles: profileNames.map((name) => {
        const profile = profiles[name]
        return {
          name,
          isDefault: name === defaultProfile,
          organization: profile.organizationShortId,
          endpoint: profile.apiEndpoint,
          auth: profile.authMethod,
        }
      }),
      configPath: configManager.configPath,
    },
  }
}
