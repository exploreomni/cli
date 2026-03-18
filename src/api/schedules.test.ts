import {
  listSchedules,
  getSchedule,
  triggerSchedule,
  pauseSchedule,
  resumeSchedule,
  deleteSchedule,
  getScheduleRecipients,
} from './schedules.js'
import type { APIClient } from './client.js'

const createMockClient = () =>
  ({
    get: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    post: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    put: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    delete: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
  }) as unknown as APIClient & {
    get: ReturnType<typeof vi.fn>
    post: ReturnType<typeof vi.fn>
    put: ReturnType<typeof vi.fn>
    delete: ReturnType<typeof vi.fn>
  }

describe('schedules API', () => {
  it('listSchedules with no params calls /api/v1/schedules', async () => {
    const client = createMockClient()
    await listSchedules(client)
    expect(client.get).toHaveBeenCalledWith('/api/v1/schedules')
  })

  it('listSchedules with status and destination builds query string', async () => {
    const client = createMockClient()
    await listSchedules(client, { status: 'active', destination: 'email' })
    expect(client.get).toHaveBeenCalledWith(
      '/api/v1/schedules?status=active&destination=email'
    )
  })

  it('getSchedule calls GET /api/v1/schedules/{id}', async () => {
    const client = createMockClient()
    await getSchedule(client, 'sched-1')
    expect(client.get).toHaveBeenCalledWith('/api/v1/schedules/sched-1')
  })

  it('triggerSchedule calls POST /api/v1/schedules/{id}/trigger', async () => {
    const client = createMockClient()
    await triggerSchedule(client, 'sched-1')
    expect(client.post).toHaveBeenCalledWith('/api/v1/schedules/sched-1/trigger')
  })

  it('pauseSchedule calls PUT /api/v1/schedules/{id}/pause', async () => {
    const client = createMockClient()
    await pauseSchedule(client, 'sched-1')
    expect(client.put).toHaveBeenCalledWith('/api/v1/schedules/sched-1/pause')
  })

  it('resumeSchedule calls PUT /api/v1/schedules/{id}/resume', async () => {
    const client = createMockClient()
    await resumeSchedule(client, 'sched-1')
    expect(client.put).toHaveBeenCalledWith('/api/v1/schedules/sched-1/resume')
  })

  it('deleteSchedule calls DELETE /api/v1/schedules/{id}', async () => {
    const client = createMockClient()
    await deleteSchedule(client, 'sched-1')
    expect(client.delete).toHaveBeenCalledWith('/api/v1/schedules/sched-1')
  })

  it('getScheduleRecipients calls GET /api/v1/schedules/{id}/recipients', async () => {
    const client = createMockClient()
    await getScheduleRecipients(client, 'sched-1')
    expect(client.get).toHaveBeenCalledWith('/api/v1/schedules/sched-1/recipients')
  })
})
