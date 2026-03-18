vi.mock('../../config/index.js', () => ({
  getConfigManager: vi.fn(() => ({
    getProfile: vi.fn(() => ({ apiEndpoint: 'https://test.omni.co', organizationId: 'org-1' })),
  })),
  getAuthContext: vi.fn(() => ({ token: 'test-token', organizationId: 'org-1' })),
  validateAuth: vi.fn(() => null),
}))

vi.mock('../../api/index.js', () => ({
  createAPIClient: vi.fn(() => ({})),
  generateQuery: vi.fn(),
}))

import { executeQueryRun } from './run.execute.js'
import { getConfigManager, validateAuth } from '../../config/index.js'
import { generateQuery } from '../../api/index.js'

const defaultOpts = { prompt: 'show me revenue', modelId: 'model-1' }

describe('executeQueryRun', () => {
  it('returns columns, rows, topic, and rowCount on success', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: {
        query: { modelId: 'model-1', fields: [{ name: 'revenue', kind: 'metric' }] },
        topic: 'Sales',
        result: {
          columns: ['revenue'],
          rows: [[100], [200], [300]],
        },
      },
      error: undefined,
      status: 200,
    })

    const result = await executeQueryRun(defaultOpts)

    expect(result.data.columns).toEqual(['revenue'])
    expect(result.data.rows).toEqual([[100], [200], [300]])
    expect(result.data.topic).toBe('Sales')
    expect(result.data.rowCount).toBe(3)
  })

  it('throws on API-level error', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: undefined,
      error: 'Service unavailable',
      status: 503,
    })

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow('Service unavailable')
  })

  it('throws detail on data-level error with detail', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: {
        error: { message: 'Query failed', detail: 'Column not found: foo' },
        query: null,
      },
      error: undefined,
      status: 200,
    })

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow('Column not found: foo')
  })

  it('throws message on data-level error without detail', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: {
        error: { message: 'Query failed' },
        query: null,
      },
      error: undefined,
      status: 200,
    })

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow('Query failed')
  })

  it('throws when no query is generated', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: {
        query: null,
      },
      error: undefined,
      status: 200,
    })

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow(
      'No query generated. Try rephrasing your question.'
    )
  })

  it('returns empty results when result has no rows', async () => {
    vi.mocked(generateQuery).mockResolvedValue({
      data: {
        query: { modelId: 'model-1', fields: [] },
        result: { columns: [], rows: [] },
      },
      error: undefined,
      status: 200,
    })

    const result = await executeQueryRun(defaultOpts)

    expect(result.data.columns).toEqual([])
    expect(result.data.rows).toEqual([])
    expect(result.data.rowCount).toBe(0)
    expect(result.data.topic).toBeNull()
  })

  it('throws when no profile is configured', async () => {
    vi.mocked(getConfigManager).mockReturnValueOnce({
      getProfile: vi.fn(() => null),
    } as any)

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow('No profile configured')
  })

  it('throws on auth error', async () => {
    vi.mocked(validateAuth).mockReturnValueOnce('Token expired')

    await expect(executeQueryRun(defaultOpts)).rejects.toThrow('Token expired')
  })
})
