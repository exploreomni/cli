import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, getScheduleRecipients } from '../../api/index.js'
import type { RecipientsResponse } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ScheduleRecipientsResult {
  type: string
  recipients: Array<{
    name: string
    email: string
    id: string
    group?: string
  }>
}

export const executeScheduleRecipients = async (options: {
  scheduleId: string
  profile?: string
}): Promise<CommandResult<ScheduleRecipientsResult>> => {
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
  const result = await getScheduleRecipients(client, options.scheduleId)

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  const allRecipients: ScheduleRecipientsResult['recipients'] = []

  if (result.data.recipients) {
    for (const r of result.data.recipients) {
      allRecipients.push({
        name: r.name,
        email: r.email,
        id: r.id,
      })
    }
  }

  if (result.data.userGroupRecipients) {
    for (const group of result.data.userGroupRecipients) {
      for (const r of group.recipients) {
        allRecipients.push({
          name: r.name,
          email: r.email,
          id: r.id,
          group: group.name,
        })
      }
    }
  }

  return {
    data: {
      type: result.data.type,
      recipients: allRecipients,
    },
  }
}
