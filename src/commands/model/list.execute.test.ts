vi.mock('../../config/index.js', () => ({
  getConfigManager: vi.fn(() => ({
    getProfile: vi.fn(() => ({
      apiEndpoint: 'https://test.omni.co',
      organizationId: 'org-1',
    })),
  })),
  getAuthContext: vi.fn(() => ({
    token: 'test-token',
    organizationId: 'org-1',
  })),
  validateAuth: vi.fn(() => null),
}))

vi.mock('../../api/index.js', () => ({
  createAPIClient: vi.fn(() => ({})),
  listModels: vi.fn(),
}))

import { listModels } from '../../api/index.js'
import { getConfigManager, validateAuth } from '../../config/index.js'
import { executeModelList, formatDate } from './list.execute.js'

describe('formatDate', () => {
  it('returns "just now" for less than 1 hour ago', () => {
    const recent = new Date(Date.now() - 10 * 60 * 1000).toISOString()
    expect(formatDate(recent)).toBe('just now')
  })

  it('returns "Xh ago" for less than 24 hours', () => {
    const hoursAgo = new Date(Date.now() - 5 * 60 * 60 * 1000).toISOString()
    expect(formatDate(hoursAgo)).toBe('5h ago')
  })

  it('returns "Xd ago" for less than 7 days', () => {
    const daysAgo = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString()
    expect(formatDate(daysAgo)).toBe('3d ago')
  })

  it('returns locale date for >= 7 days', () => {
    const old = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)
    expect(formatDate(old.toISOString())).toBe(old.toLocaleDateString())
  })
})

describe('executeModelList', () => {
  beforeEach(() => {
    vi.mocked(listModels).mockResolvedValue({
      data: {
        records: [
          {
            id: 'abcdefghijklmnop',
            name: 'My Model',
            modelKind: 'shared',
            connectionId: null,
            baseModelId: null,
            createdAt: new Date().toISOString(),
            updatedAt: new Date().toISOString(),
            deletedAt: null,
          },
          {
            id: '1234567890abcdef',
            name: null,
            modelKind: null,
            connectionId: null,
            baseModelId: null,
            createdAt: new Date().toISOString(),
            updatedAt: new Date().toISOString(),
            deletedAt: null,
          },
        ],
        pageInfo: {
          hasNextPage: false,
          nextCursor: null,
          pageSize: 20,
          totalRecords: 2,
        },
      },
      error: undefined,
      status: 200,
    })
  })

  it('returns models with correct transformations', async () => {
    const result = await executeModelList({})

    expect(result.data.models).toHaveLength(2)
    expect(result.data.count).toBe(2)

    // First model: normal values
    expect(result.data.models[0].name).toBe('My Model')
    expect(result.data.models[0].kind).toBe('shared')
    expect(result.data.models[0].id).toBe('abcdefgh')

    // Second model: fallbacks
    expect(result.data.models[1].name).toBe('(unnamed)')
    expect(result.data.models[1].kind).toBe('-')
    expect(result.data.models[1].id).toBe('12345678')
  })

  it('throws when no profile is configured', async () => {
    vi.mocked(getConfigManager).mockReturnValueOnce({
      getProfile: vi.fn(() => null),
    } as any)

    await expect(executeModelList({})).rejects.toThrow('No profile configured')
  })

  it('throws on auth failure', async () => {
    vi.mocked(validateAuth).mockReturnValueOnce('Invalid token')

    await expect(executeModelList({})).rejects.toThrow('Invalid token')
  })

  it('throws on API error', async () => {
    vi.mocked(listModels).mockResolvedValueOnce({
      data: undefined,
      error: 'Server error',
      status: 500,
    })

    await expect(executeModelList({})).rejects.toThrow('Server error')
  })

  it('throws when no data is returned', async () => {
    vi.mocked(listModels).mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      status: 200,
    })

    await expect(executeModelList({})).rejects.toThrow('No data returned')
  })
})
