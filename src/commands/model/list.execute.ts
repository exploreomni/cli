import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, listModels } from '../../api/index.js'
import type { ModelMetadata } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ModelListResult {
  models: Array<{
    name: string
    kind: string
    updated: string
    id: string
    raw: ModelMetadata
  }>
  count: number
}

export const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffHours < 1) return 'just now'
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

export const executeModelList = async (options: {
  modelKind?: string
  profile?: string
}): Promise<CommandResult<ModelListResult>> => {
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

  const result = await listModels(client, {
    modelKind: options.modelKind as 'shared' | 'schema' | undefined,
  })

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  const models = result.data.records.map((model) => ({
    name: model.name ?? '(unnamed)',
    kind: model.modelKind ?? '-',
    updated: formatDate(model.updatedAt),
    id: model.id.slice(0, 8),
    raw: model,
  }))

  return {
    data: { models, count: models.length },
  }
}
