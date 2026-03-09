import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, listSchedules } from '../../api/index.js'
import type { ScheduleListItem } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ScheduleListResult {
  schedules: ScheduleListItem[]
  totalRecords: number
  hasNextPage: boolean
}

const formatStatus = (item: ScheduleListItem): string => {
  if (item.systemDisabledAt) return 'system-disabled'
  if (item.disabledAt) return 'paused'
  return 'active'
}

const formatLastRun = (item: ScheduleListItem): string => {
  if (!item.lastCompletedAt) return '-'
  const date = new Date(item.lastCompletedAt)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffHours < 1) return 'just now'
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

export const formatScheduleRow = (
  item: ScheduleListItem
): Record<string, string> => ({
  name: item.name,
  dashboard: item.dashboardName,
  owner: item.ownerName,
  destination: item.destinationType,
  format: item.format,
  status: formatStatus(item),
  lastRun: formatLastRun(item),
  lastStatus: item.lastStatus ?? '-',
  id: item.id.slice(0, 8),
})

export const executeScheduleList = async (options: {
  status?: string
  destination?: string
  search?: string
  sort?: string
  sortDirection?: string
  pageSize?: number
  page?: number
  profile?: string
}): Promise<CommandResult<ScheduleListResult>> => {
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

  const result = await listSchedules(client, {
    status: options.status,
    destination: options.destination,
    searchTerm: options.search,
    sortField: options.sort,
    sortDirection: options.sortDirection,
    pageSize: options.pageSize,
    cursor: options.page,
  })

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  // The API returns one row per schedule+destination pair, so deduplicate by ID
  const seen = new Set<string>()
  const schedules = result.data.records.filter((s) => {
    if (seen.has(s.id)) return false
    seen.add(s.id)
    return true
  })

  return {
    data: {
      schedules,
      totalRecords: schedules.length,
      hasNextPage: result.data.pageInfo.hasNextPage,
    },
  }
}
