import type { APIClient } from './client.js'
import { getModelYaml, listModels, validateModel } from './models.js'

const createMockClient = () =>
  ({
    get: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    post: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    put: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
    delete: vi.fn().mockResolvedValue({ data: {}, status: 200 }),
  }) as unknown as APIClient & { get: ReturnType<typeof vi.fn> }

describe('models API', () => {
  it('listModels with no params calls /api/v1/models', async () => {
    const client = createMockClient()
    await listModels(client)
    expect(client.get).toHaveBeenCalledWith('/api/v1/models')
  })

  it('listModels with modelKind and pageSize builds correct query string', async () => {
    const client = createMockClient()
    await listModels(client, { modelKind: 'schema', pageSize: 50 })
    expect(client.get).toHaveBeenCalledWith(
      '/api/v1/models?modelKind=schema&pageSize=50'
    )
  })

  it('validateModel with just modelId calls correct path', async () => {
    const client = createMockClient()
    await validateModel(client, 'model-123')
    expect(client.get).toHaveBeenCalledWith('/api/v1/models/model-123/validate')
  })

  it('validateModel with branchId adds query param', async () => {
    const client = createMockClient()
    await validateModel(client, 'model-123', 'branch-456')
    expect(client.get).toHaveBeenCalledWith(
      '/api/v1/models/model-123/validate?branchId=branch-456'
    )
  })

  it('getModelYaml calls correct path', async () => {
    const client = createMockClient()
    await getModelYaml(client, 'model-123')
    expect(client.get).toHaveBeenCalledWith('/api/v1/models/model-123/yaml')
  })
})
