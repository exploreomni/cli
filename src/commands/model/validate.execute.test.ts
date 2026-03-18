vi.mock('../../config/index.js', () => ({
  getConfigManager: vi.fn(() => ({
    getProfile: vi.fn(() => ({ apiEndpoint: 'https://test.omni.co', organizationId: 'org-1' })),
  })),
  getAuthContext: vi.fn(() => ({ token: 'test-token', organizationId: 'org-1' })),
  validateAuth: vi.fn(() => null),
}))

vi.mock('../../api/index.js', () => ({
  createAPIClient: vi.fn(() => ({})),
  validateModel: vi.fn(),
}))

import { executeModelValidate } from './validate.execute.js'
import { getConfigManager } from '../../config/index.js'
import { validateModel } from '../../api/index.js'

describe('executeModelValidate', () => {
  it('returns valid result with no issues', async () => {
    vi.mocked(validateModel).mockResolvedValue({
      data: { valid: true, issues: [] },
      error: undefined,
      status: 200,
    })

    const result = await executeModelValidate({ modelId: 'model-1' })

    expect(result.exitCode).toBe(0)
    expect(result.data.valid).toBe(true)
    expect(result.data.errorCount).toBe(0)
    expect(result.data.warningCount).toBe(0)
    expect(result.data.infoCount).toBe(0)
  })

  it('returns invalid result with correct counts', async () => {
    vi.mocked(validateModel).mockResolvedValue({
      data: {
        valid: false,
        issues: [
          { level: 'error', message: 'err1', path: '/a' },
          { level: 'error', message: 'err2', path: '/b' },
          { level: 'warning', message: 'warn1', path: '/c' },
          { level: 'info', message: 'info1', path: '/d' },
        ],
      },
      error: undefined,
      status: 200,
    })

    const result = await executeModelValidate({ modelId: 'model-1' })

    expect(result.exitCode).toBe(1)
    expect(result.data.valid).toBe(false)
    expect(result.data.errorCount).toBe(2)
    expect(result.data.warningCount).toBe(1)
    expect(result.data.infoCount).toBe(1)
    expect(result.data.issues).toHaveLength(4)
  })

  it('throws when no profile is configured', async () => {
    vi.mocked(getConfigManager).mockReturnValueOnce({
      getProfile: vi.fn(() => null),
    } as any)

    await expect(executeModelValidate({ modelId: 'model-1' })).rejects.toThrow(
      'No profile configured'
    )
  })

  it('throws on API error', async () => {
    vi.mocked(validateModel).mockResolvedValueOnce({
      data: undefined,
      error: 'Validation service unavailable',
      status: 500,
    })

    await expect(executeModelValidate({ modelId: 'model-1' })).rejects.toThrow(
      'Validation service unavailable'
    )
  })

  it('throws when no data is returned', async () => {
    vi.mocked(validateModel).mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      status: 200,
    })

    await expect(executeModelValidate({ modelId: 'model-1' })).rejects.toThrow(
      'No data returned'
    )
  })
})
