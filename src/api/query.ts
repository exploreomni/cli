import type { APIClient, APIResponse } from './client.js'

export interface GenerateQueryParams {
  modelId: string
  prompt: string
  branchId?: string
  topicName?: string
  runQuery?: boolean
}

export interface QueryField {
  name: string
  kind: string
}

export interface GeneratedQuery {
  modelId: string
  fields: QueryField[]
  filters?: unknown[]
  sorts?: unknown[]
  limit?: number
}

export interface QueryResult {
  columns: string[]
  rows: unknown[][]
}

export interface GenerateQueryResponse {
  query: GeneratedQuery | null
  topic?: string
  result?: QueryResult
  error?: {
    message: string
    detail?: string
  }
}

export const generateQuery = async (
  client: APIClient,
  params: GenerateQueryParams
): Promise<APIResponse<GenerateQueryResponse>> => {
  return client.post<GenerateQueryResponse>('/api/v1/ai/generate-query', {
    modelId: params.modelId,
    prompt: params.prompt,
    branchId: params.branchId,
    currentTopicName: params.topicName,
    runQuery: params.runQuery ?? true,
  })
}
