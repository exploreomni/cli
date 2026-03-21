import { getConfigManager } from './config-manager.js'
import type { Profile } from './schema.js'

export interface AuthContext {
  apiKey?: string
  token?: string
  profile?: Profile
  organizationId?: string
}

export const getAuthContext = (profileName?: string): AuthContext => {
  const token = process.env.OMNI_API_TOKEN
  const envOrgId = process.env.OMNI_ORG_ID

  const configManager = getConfigManager()
  const profile = configManager.getProfile(profileName)

  const apiKey = process.env.OMNI_API_KEY ?? profile?.apiKey

  return {
    apiKey,
    token,
    profile,
    organizationId: envOrgId ?? profile?.organizationId,
  }
}

export const getAuthHeaders = (
  context: AuthContext
): Record<string, string> => {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (context.token) {
    headers.Authorization = `Bearer ${context.token}`
  } else if (context.apiKey) {
    headers.Authorization = `Bearer ${context.apiKey}`
  }

  return headers
}

export const validateAuth = (context: AuthContext): string | null => {
  if (!context.apiKey && !context.token && !context.profile) {
    return 'No authentication configured. Run `omni-cli config init` to set up a profile or set OMNI_API_KEY environment variable.'
  }

  if (!context.organizationId) {
    return 'No organization ID configured. Set OMNI_ORG_ID environment variable or configure a profile.'
  }

  return null
}
