import type { APIClient, APIResponse } from './client.js'
import type {
  ModelYamlResponse,
  PaginatedModelsResponse,
  ValidationResponse,
} from './types.js'

export interface ListModelsParams {
  modelKind?: 'schema' | 'shared' | 'shared_extension' | 'branch'
  connectionId?: string
  pageSize?: number
  cursor?: string
}

export const listModels = async (
  client: APIClient,
  params: ListModelsParams = {}
): Promise<APIResponse<PaginatedModelsResponse>> => {
  const searchParams = new URLSearchParams()

  if (params.modelKind) {
    searchParams.set('modelKind', params.modelKind)
  }
  if (params.connectionId) {
    searchParams.set('connectionId', params.connectionId)
  }
  if (params.pageSize) {
    searchParams.set('pageSize', params.pageSize.toString())
  }
  if (params.cursor) {
    searchParams.set('cursor', params.cursor)
  }

  const query = searchParams.toString()
  const path = `/api/v1/models${query ? `?${query}` : ''}`

  return client.get<PaginatedModelsResponse>(path)
}

export const validateModel = async (
  client: APIClient,
  modelId: string,
  branchId?: string
): Promise<APIResponse<ValidationResponse>> => {
  const searchParams = new URLSearchParams()

  if (branchId) {
    searchParams.set('branchId', branchId)
  }

  const query = searchParams.toString()
  const path = `/api/v1/models/${modelId}/validate${query ? `?${query}` : ''}`

  return client.get<ValidationResponse>(path)
}

export const getModelYaml = async (
  client: APIClient,
  modelId: string
): Promise<APIResponse<ModelYamlResponse>> => {
  return client.get<ModelYamlResponse>(`/api/v1/models/${modelId}/yaml`)
}
