import type { ScheduleListItem } from '../../api/index.js'
import { createAPIClient, getSchedule } from '../../api/index.js'
import {
  getAuthContext,
  getConfigManager,
  validateAuth,
} from '../../config/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ScheduleGetResult {
  schedule: ScheduleListItem
}

export const executeScheduleGet = async (options: {
  scheduleId: string
  profile?: string
}): Promise<CommandResult<ScheduleGetResult>> => {
  const configManager = getConfigManager()
  const profileData = configManager.getProfile(options.profile)

  if (!profileData) {
    throw new Error('No profile configured. Run `omni-cli config:init` first.')
  }

  const authContext = getAuthContext(options.profile)
  const authError = validateAuth(authContext)

  if (authError) {
    throw new Error(authError)
  }

  const client = createAPIClient(profileData.apiEndpoint, authContext)
  const result = await getSchedule(client, options.scheduleId)

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  return {
    data: { schedule: result.data },
  }
}
