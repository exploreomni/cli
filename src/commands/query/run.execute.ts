import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, generateQuery } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface QueryRunResult {
  columns: string[]
  rows: unknown[][]
  topic: string | null
  rowCount: number
}

export const executeQueryRun = async (options: {
  prompt: string
  modelId: string
  topic?: string
  profile?: string
}): Promise<CommandResult<QueryRunResult>> => {
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

  const response = await generateQuery(client, {
    modelId: options.modelId,
    prompt: options.prompt,
    topicName: options.topic,
    runQuery: true,
  })

  if (response.error) {
    throw new Error(response.error)
  }

  if (response.data?.error) {
    throw new Error(
      response.data.error.detail ?? response.data.error.message
    )
  }

  if (!response.data?.query) {
    throw new Error('No query generated. Try rephrasing your question.')
  }

  const result = response.data.result
  return {
    data: {
      columns: result?.columns ?? [],
      rows: result?.rows ?? [],
      topic: response.data.topic ?? null,
      rowCount: result?.rows.length ?? 0,
    },
  }
}
