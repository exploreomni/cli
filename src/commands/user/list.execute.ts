import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, listUsers } from '../../api/index.js'
import type { UserListItem } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface UserListResult {
  users: UserListItem[]
  totalResults: number
  startIndex: number
  itemsPerPage: number
}

const formatLastLogin = (lastLogin: string | null): string => {
  if (!lastLogin) return '-'
  const date = new Date(lastLogin)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffHours < 1) return 'just now'
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

export const formatUserRow = (
  item: UserListItem
): Record<string, string> => ({
  name: item.displayName,
  email: item.email,
  groups: item.groups.map((g) => g.display).join(', ') || '-',
  active: item.active ? 'yes' : 'no',
  lastLogin: formatLastLogin(item.lastLogin),
  id: item.id.slice(0, 8),
})

export const executeUserList = async (options: {
  search?: string
  count?: number
  profile?: string
}): Promise<CommandResult<UserListResult>> => {
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

  const filter = options.search
    ? `displayName co "${options.search}" or userName co "${options.search}"`
    : undefined

  const result = await listUsers(client, {
    count: options.count,
    filter,
  })

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  return {
    data: {
      users: result.data.users,
      totalResults: result.data.totalResults,
      startIndex: result.data.startIndex,
      itemsPerPage: result.data.itemsPerPage,
    },
  }
}
