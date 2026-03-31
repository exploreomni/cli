import type { APIClient, APIResponse } from './client.js'
import type {
  RecipientsResponse,
  ScheduleListItem,
  ScheduleListResponse,
  SuccessResponse,
} from './schedule-types.js'

export interface ListSchedulesParams {
  status?: string
  destination?: string
  searchTerm?: string
  scheduleType?: string
  contentType?: string
  identifier?: string
  ownerId?: string
  sortField?: string
  sortDirection?: string
  cursor?: number
  pageSize?: number
}

export const listSchedules = async (
  client: APIClient,
  params: ListSchedulesParams = {}
): Promise<APIResponse<ScheduleListResponse>> => {
  const searchParams = new URLSearchParams()

  if (params.status) searchParams.set('status', params.status)
  if (params.destination) searchParams.set('destination', params.destination)
  if (params.searchTerm) searchParams.set('searchTerm', params.searchTerm)
  if (params.scheduleType) searchParams.set('scheduleType', params.scheduleType)
  if (params.contentType) searchParams.set('contentType', params.contentType)
  if (params.identifier) searchParams.set('identifier', params.identifier)
  if (params.ownerId) searchParams.set('ownerId', params.ownerId)
  if (params.sortField) searchParams.set('sortField', params.sortField)
  if (params.sortDirection)
    searchParams.set('sortDirection', params.sortDirection)
  if (params.cursor) searchParams.set('cursor', params.cursor.toString())
  if (params.pageSize) searchParams.set('pageSize', params.pageSize.toString())

  const query = searchParams.toString()
  const path = `/api/v1/schedules${query ? `?${query}` : ''}`

  return client.get<ScheduleListResponse>(path)
}

export const getSchedule = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<ScheduleListItem>> => {
  return client.get<ScheduleListItem>(`/api/v1/schedules/${scheduleId}`)
}

export const triggerSchedule = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<SuccessResponse>> => {
  return client.post<SuccessResponse>(`/api/v1/schedules/${scheduleId}/trigger`)
}

export const pauseSchedule = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<SuccessResponse>> => {
  return client.put<SuccessResponse>(`/api/v1/schedules/${scheduleId}/pause`)
}

export const resumeSchedule = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<SuccessResponse>> => {
  return client.put<SuccessResponse>(`/api/v1/schedules/${scheduleId}/resume`)
}

export const deleteSchedule = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<SuccessResponse>> => {
  return client.delete<SuccessResponse>(`/api/v1/schedules/${scheduleId}`)
}

export const getScheduleRecipients = async (
  client: APIClient,
  scheduleId: string
): Promise<APIResponse<RecipientsResponse>> => {
  return client.get<RecipientsResponse>(
    `/api/v1/schedules/${scheduleId}/recipients`
  )
}
