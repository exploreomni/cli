import {
  getConfigManager,
  getAuthContext,
  validateAuth,
} from '../../config/index.js'
import { createAPIClient, validateModel } from '../../api/index.js'
import type { ValidationIssue } from '../../api/index.js'
import type { CommandResult } from '../../output/index.js'

export interface ModelValidateResult {
  valid: boolean
  issues: ValidationIssue[]
  errorCount: number
  warningCount: number
  infoCount: number
}

export const executeModelValidate = async (options: {
  modelId: string
  branchId?: string
  profile?: string
}): Promise<CommandResult<ModelValidateResult>> => {
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
  const result = await validateModel(client, options.modelId, options.branchId)

  if (result.error) {
    throw new Error(result.error)
  }

  if (!result.data) {
    throw new Error('No data returned')
  }

  const issues = result.data.issues
  return {
    data: {
      valid: result.data.valid,
      issues,
      errorCount: issues.filter((i) => i.level === 'error').length,
      warningCount: issues.filter((i) => i.level === 'warning').length,
      infoCount: issues.filter((i) => i.level === 'info').length,
    },
    exitCode: result.data.valid ? 0 : 1,
  }
}
