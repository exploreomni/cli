import type { APIClient, APIResponse } from './client.js'
import type { UserListResponse } from './user-types.js'

const SCIM_EXTENSION_URN = 'urn:omni:params:scim:schemas:extension:user:2.0'

export interface ListUsersParams {
  startIndex?: number
  count?: number
  filter?: string
}

interface ScimGroup {
  display: string
  value: string
}

interface ScimResource {
  id: string
  userName: string
  displayName: string
  active: boolean
  groups?: ScimGroup[]
  [key: string]: unknown
}

interface ScimListResponse {
  Resources: ScimResource[]
  totalResults: number
  startIndex: number
  itemsPerPage: number
}

export const listUsers = async (
  client: APIClient,
  params: ListUsersParams = {}
): Promise<APIResponse<UserListResponse>> => {
  const searchParams = new URLSearchParams()

  if (params.startIndex)
    searchParams.set('startIndex', params.startIndex.toString())
  if (params.count) searchParams.set('count', params.count.toString())
  if (params.filter) searchParams.set('filter', params.filter)

  const query = searchParams.toString()
  const path = `/api/scim/v2/Users${query ? `?${query}` : ''}`

  const result = await client.get<ScimListResponse>(path)

  if (result.error || !result.data) {
    return { error: result.error, status: result.status }
  }

  const users = result.data.Resources.map((r) => {
    const extension = r[SCIM_EXTENSION_URN] as
      | { lastLogin?: string }
      | undefined

    return {
      id: r.id,
      displayName: r.displayName ?? r.userName,
      email: r.userName,
      active: r.active,
      groups: r.groups ?? [],
      lastLogin: extension?.lastLogin ?? null,
    }
  })

  return {
    data: {
      users,
      totalResults: result.data.totalResults,
      startIndex: result.data.startIndex,
      itemsPerPage: result.data.itemsPerPage,
    },
    status: result.status,
  }
}
