vi.mock('../../config/index.js', () => ({
  getConfigManager: vi.fn(() => ({
    getProfile: vi.fn(() => ({ apiEndpoint: 'https://test.omni.co', organizationId: 'org-1' })),
  })),
  getAuthContext: vi.fn(() => ({ token: 'test-token', organizationId: 'org-1' })),
  validateAuth: vi.fn(() => null),
}))

vi.mock('../../api/index.js', () => ({
  createAPIClient: vi.fn(() => ({})),
  getScheduleRecipients: vi.fn(),
}))

import { executeScheduleRecipients } from './recipients.execute.js'
import { getConfigManager } from '../../config/index.js'
import { getScheduleRecipients } from '../../api/index.js'

const defaultOpts = { scheduleId: 'sched-1' }

describe('executeScheduleRecipients', () => {
  it('flattens direct recipients', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValue({
      data: {
        type: 'email',
        recipients: [
          { id: 'r1', name: 'Alice', email: 'alice@test.com' },
          { id: 'r2', name: 'Bob', email: 'bob@test.com' },
        ],
        userGroupRecipients: [],
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleRecipients(defaultOpts)

    expect(result.data.recipients).toHaveLength(2)
    expect(result.data.recipients[0]).toEqual({ id: 'r1', name: 'Alice', email: 'alice@test.com' })
    expect(result.data.recipients[1]).toEqual({ id: 'r2', name: 'Bob', email: 'bob@test.com' })
  })

  it('flattens group recipients with group field', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValue({
      data: {
        type: 'email',
        recipients: [],
        userGroupRecipients: [
          {
            id: 'g1',
            name: 'Engineering',
            recipients: [
              { id: 'r3', name: 'Charlie', email: 'charlie@test.com' },
            ],
          },
        ],
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleRecipients(defaultOpts)

    expect(result.data.recipients).toHaveLength(1)
    expect(result.data.recipients[0]).toEqual({
      id: 'r3',
      name: 'Charlie',
      email: 'charlie@test.com',
      group: 'Engineering',
    })
  })

  it('handles missing recipients array', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValue({
      data: {
        type: 'email',
        recipients: undefined,
        userGroupRecipients: undefined,
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleRecipients(defaultOpts)

    expect(result.data.recipients).toHaveLength(0)
  })

  it('handles missing userGroupRecipients array', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValue({
      data: {
        type: 'email',
        recipients: [{ id: 'r1', name: 'Alice', email: 'alice@test.com' }],
        userGroupRecipients: undefined,
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleRecipients(defaultOpts)

    expect(result.data.recipients).toHaveLength(1)
  })

  it('combines direct and group recipients', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValue({
      data: {
        type: 'email',
        recipients: [{ id: 'r1', name: 'Alice', email: 'alice@test.com' }],
        userGroupRecipients: [
          {
            id: 'g1',
            name: 'Team',
            recipients: [{ id: 'r2', name: 'Bob', email: 'bob@test.com' }],
          },
        ],
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleRecipients(defaultOpts)

    expect(result.data.recipients).toHaveLength(2)
    expect(result.data.recipients[0].group).toBeUndefined()
    expect(result.data.recipients[1].group).toBe('Team')
  })

  it('throws when no profile is configured', async () => {
    vi.mocked(getConfigManager).mockReturnValueOnce({
      getProfile: vi.fn(() => null),
    } as any)

    await expect(executeScheduleRecipients(defaultOpts)).rejects.toThrow('No profile configured')
  })

  it('throws on API error', async () => {
    vi.mocked(getScheduleRecipients).mockResolvedValueOnce({
      data: undefined,
      error: 'Not found',
      status: 404,
    })

    await expect(executeScheduleRecipients(defaultOpts)).rejects.toThrow('Not found')
  })
})
