vi.mock('../../config/index.js', () => ({
  getConfigManager: vi.fn(() => ({
    getProfile: vi.fn(() => ({ apiEndpoint: 'https://test.omni.co', organizationId: 'org-1' })),
  })),
  getAuthContext: vi.fn(() => ({ token: 'test-token', organizationId: 'org-1' })),
  validateAuth: vi.fn(() => null),
}))

vi.mock('../../api/index.js', () => ({
  createAPIClient: vi.fn(() => ({})),
  listSchedules: vi.fn(),
}))

import { formatScheduleRow, executeScheduleList } from './list.execute.js'
import { getConfigManager } from '../../config/index.js'
import { listSchedules } from '../../api/index.js'
import type { ScheduleListItem } from '../../api/index.js'

const makeScheduleItem = (overrides: Partial<ScheduleListItem> = {}): ScheduleListItem => ({
  id: 'sched-123-full-uuid',
  name: 'Test Schedule',
  dashboardName: 'Dashboard',
  ownerName: 'Owner',
  ownerId: 'owner-1',
  destinationType: 'email',
  format: 'pdf',
  schedule: '0 9 * * 1',
  timezone: 'UTC',
  content: 'full',
  identifier: 'id-1',
  disabledAt: null,
  lastStatus: null,
  lastCompletedAt: null,
  recipientCount: 5,
  systemDisabledAt: null,
  systemDisabledReason: null,
  slackRecipientType: null,
  ...overrides,
})

describe('formatScheduleRow', () => {
  it('returns active status when not disabled', () => {
    const row = formatScheduleRow(makeScheduleItem())
    expect(row.status).toBe('active')
  })

  it('returns paused status when disabledAt is set', () => {
    const row = formatScheduleRow(makeScheduleItem({ disabledAt: '2025-01-01T00:00:00Z' }))
    expect(row.status).toBe('paused')
  })

  it('returns system-disabled status when systemDisabledAt is set', () => {
    const row = formatScheduleRow(
      makeScheduleItem({ systemDisabledAt: '2025-01-01T00:00:00Z' })
    )
    expect(row.status).toBe('system-disabled')
  })

  it('returns "-" for lastRun when no lastCompletedAt', () => {
    const row = formatScheduleRow(makeScheduleItem())
    expect(row.lastRun).toBe('-')
  })

  it('shows relative time for recent lastCompletedAt', () => {
    const recent = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString()
    const row = formatScheduleRow(makeScheduleItem({ lastCompletedAt: recent }))
    expect(row.lastRun).toBe('3h ago')
  })

  it('truncates ID to 8 chars', () => {
    const row = formatScheduleRow(makeScheduleItem())
    expect(row.id).toBe('sched-12')
  })
})

describe('executeScheduleList', () => {
  it('returns deduplicated schedules', async () => {
    const item = makeScheduleItem()
    vi.mocked(listSchedules).mockResolvedValue({
      data: {
        records: [item, item, { ...item, id: 'different-id-full' }],
        pageInfo: { hasNextPage: false, pageSize: 20, totalRecords: 3 },
      },
      error: undefined,
      status: 200,
    })

    const result = await executeScheduleList({})

    expect(result.data.schedules).toHaveLength(2)
    expect(result.data.totalRecords).toBe(2)
  })

  it('throws when no profile is configured', async () => {
    vi.mocked(getConfigManager).mockReturnValueOnce({
      getProfile: vi.fn(() => null),
    } as any)

    await expect(executeScheduleList({})).rejects.toThrow('No profile configured')
  })

  it('throws on API error', async () => {
    vi.mocked(listSchedules).mockResolvedValueOnce({
      data: undefined,
      error: 'Connection failed',
      status: 500,
    })

    await expect(executeScheduleList({})).rejects.toThrow('Connection failed')
  })
})
