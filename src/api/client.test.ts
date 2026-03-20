vi.mock('../config/index.js', () => ({
  getAuthHeaders: () => ({
    'Content-Type': 'application/json',
    Authorization: 'Bearer test-token',
  }),
}))

import { APIClient, createAPIClient } from './client.js'

const mockAuthContext = {
  type: 'token' as const,
  token: 'test-token',
}

const mockResponse = (status: number, body: unknown, ok?: boolean) =>
  Promise.resolve({
    ok: ok ?? (status >= 200 && status < 300),
    status,
    json: () => Promise.resolve(body),
    text: () =>
      Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)),
  } as Response)

let fetchSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  fetchSpy = vi
    .spyOn(globalThis, 'fetch')
    .mockResolvedValue(undefined as unknown as Response)
})

afterEach(() => {
  fetchSpy.mockRestore()
})

describe('APIClient', () => {
  it('strips trailing slash from baseUrl', () => {
    const client = createAPIClient('https://api.example.com/', mockAuthContext)
    expect((client as any).baseUrl).toBe('https://api.example.com')
  })

  it('GET request sends correct method, url, and headers', async () => {
    fetchSpy.mockReturnValueOnce(mockResponse(200, { result: 'ok' }))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    await client.get('/api/v1/test')

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/test',
      {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
          Authorization: 'Bearer test-token',
        },
        body: undefined,
      }
    )
  })

  it('POST sends body as JSON', async () => {
    fetchSpy.mockReturnValueOnce(mockResponse(200, { id: '1' }))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    await client.post('/api/v1/items', { name: 'test' })

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/items',
      {
        method: 'POST',
        headers: expect.any(Object),
        body: JSON.stringify({ name: 'test' }),
      }
    )
  })

  it('PUT sends body', async () => {
    fetchSpy.mockReturnValueOnce(mockResponse(200, { updated: true }))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    await client.put('/api/v1/items/1', { name: 'updated' })

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/items/1',
      {
        method: 'PUT',
        headers: expect.any(Object),
        body: JSON.stringify({ name: 'updated' }),
      }
    )
  })

  it('DELETE sends correct method', async () => {
    fetchSpy.mockReturnValueOnce(mockResponse(200, { deleted: true }))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    await client.delete('/api/v1/items/1')

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/items/1',
      {
        method: 'DELETE',
        headers: expect.any(Object),
        body: undefined,
      }
    )
  })

  it('successful response returns data and status', async () => {
    fetchSpy.mockReturnValueOnce(mockResponse(200, { id: '1', name: 'test' }))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get<{ id: string; name: string }>(
      '/api/v1/items/1'
    )

    expect(result).toEqual({
      data: { id: '1', name: 'test' },
      status: 200,
    })
  })

  it('error response with JSON { message } extracts message', async () => {
    fetchSpy.mockReturnValueOnce(
      mockResponse(400, { message: 'Bad request' }, false)
    )
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/fail')

    expect(result.error).toBe('400: Bad request')
    expect(result.status).toBe(400)
    expect(result.data).toBeUndefined()
  })

  it('error response with JSON { error } extracts error', async () => {
    fetchSpy.mockReturnValueOnce(
      mockResponse(403, { error: 'Forbidden' }, false)
    )
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/fail')

    expect(result.error).toBe('403: Forbidden')
  })

  it('error response with JSON { error: { message } } extracts nested message', async () => {
    fetchSpy.mockReturnValueOnce(
      mockResponse(500, { error: { message: 'nested' } }, false)
    )
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/fail')

    expect(result.error).toBe('500: nested')
  })

  it('plain text error body', async () => {
    fetchSpy.mockReturnValueOnce(
      Promise.resolve({
        ok: false,
        status: 502,
        text: () => Promise.resolve('Bad Gateway'),
      } as Response)
    )
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/fail')

    expect(result.error).toBe('502: Bad Gateway')
  })

  it('network failure with "fetch failed" returns friendly connection error', async () => {
    fetchSpy.mockRejectedValueOnce(new Error('fetch failed'))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/test')

    expect(result.error).toBe(
      'Could not connect to https://api.example.com — is the server running?'
    )
    expect(result.status).toBe(0)
  })

  it('non-fetch network error returns generic error message', async () => {
    fetchSpy.mockRejectedValueOnce(new Error('DNS resolution failed'))
    const client = createAPIClient('https://api.example.com', mockAuthContext)
    const result = await client.get('/api/v1/test')

    expect(result.error).toBe(
      'Request to https://api.example.com/api/v1/test failed: DNS resolution failed'
    )
    expect(result.status).toBe(0)
  })
})
