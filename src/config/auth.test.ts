import type { AuthContext } from './auth.js'
import { getAuthHeaders, validateAuth } from './auth.js'

describe('validateAuth', () => {
  it('returns null when apiKey + organizationId present', () => {
    const context: AuthContext = { apiKey: 'key-123', organizationId: 'org-1' }
    expect(validateAuth(context)).toBeNull()
  })

  it('returns null when token + organizationId present', () => {
    const context: AuthContext = { token: 'tok-123', organizationId: 'org-1' }
    expect(validateAuth(context)).toBeNull()
  })

  it('returns null when profile + organizationId present', () => {
    const context: AuthContext = { profile: {} as any, organizationId: 'org-1' }
    expect(validateAuth(context)).toBeNull()
  })

  it('returns auth error when no apiKey, no token, no profile', () => {
    const context: AuthContext = { organizationId: 'org-1' }
    expect(validateAuth(context)).toContain('No authentication configured')
  })

  it('returns org error when apiKey present but no organizationId', () => {
    const context: AuthContext = { apiKey: 'key-123' }
    expect(validateAuth(context)).toContain('No organization ID configured')
  })
})

describe('getAuthHeaders', () => {
  it('returns Authorization with Bearer token when token present', () => {
    const context: AuthContext = { token: 'tok-123' }
    const headers = getAuthHeaders(context)
    expect(headers['Authorization']).toBe('Bearer tok-123')
  })

  it('returns Authorization with Bearer apiKey when apiKey present and no token', () => {
    const context: AuthContext = { apiKey: 'key-123' }
    const headers = getAuthHeaders(context)
    expect(headers['Authorization']).toBe('Bearer key-123')
  })

  it('token takes precedence over apiKey', () => {
    const context: AuthContext = { token: 'tok-123', apiKey: 'key-123' }
    const headers = getAuthHeaders(context)
    expect(headers['Authorization']).toBe('Bearer tok-123')
  })

  it('no Authorization header when neither present', () => {
    const context: AuthContext = {}
    const headers = getAuthHeaders(context)
    expect(headers['Authorization']).toBeUndefined()
  })

  it('always includes Content-Type', () => {
    const context: AuthContext = {}
    const headers = getAuthHeaders(context)
    expect(headers['Content-Type']).toBe('application/json')
  })
})
