import { createAPIClient, deleteSchedule } from '../../api/index.js'
import {
  getAuthContext,
  getConfigManager,
  validateAuth,
} from '../../config/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ScheduleDeleteResult {
  success: boolean
  scheduleId: string
}

export const executeScheduleDelete = async (options: {
  scheduleId: string
  profile?: string
}): Promise<CommandResult<ScheduleDeleteResult>> => {
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
  const result = await deleteSchedule(client, options.scheduleId)

  if (result.error) {
    throw new Error(result.error)
  }

  return {
    data: { success: true, scheduleId: options.scheduleId },
  }
}
