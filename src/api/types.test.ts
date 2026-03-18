import {
  ModelMetadataSchema,
  PaginatedModelsResponseSchema,
  ValidationResponseSchema,
} from './types.js'

describe('ModelMetadataSchema', () => {
  const validModel = {
    id: 'model-1',
    name: 'My Model',
    modelKind: 'schema',
    connectionId: 'conn-1',
    baseModelId: null,
    createdAt: '2025-01-01T00:00:00Z',
    updatedAt: '2025-01-02T00:00:00Z',
    deletedAt: null,
  }

  it('parses valid data', () => {
    const result = ModelMetadataSchema.parse(validModel)
    expect(result).toEqual(validModel)
  })

  it('fails when required field (id) is missing', () => {
    const { id, ...withoutId } = validModel
    expect(() => ModelMetadataSchema.parse(withoutId)).toThrow()
  })

  it('accepts null for nullable fields', () => {
    const withNulls = {
      ...validModel,
      name: null,
      modelKind: null,
      connectionId: null,
      baseModelId: null,
      deletedAt: null,
    }
    const result = ModelMetadataSchema.parse(withNulls)
    expect(result.name).toBeNull()
    expect(result.modelKind).toBeNull()
    expect(result.connectionId).toBeNull()
    expect(result.baseModelId).toBeNull()
    expect(result.deletedAt).toBeNull()
  })
})

describe('PaginatedModelsResponseSchema', () => {
  it('parses a valid paginated response', () => {
    const data = {
      pageInfo: {
        hasNextPage: true,
        nextCursor: 'abc123',
        pageSize: 20,
        totalRecords: 100,
      },
      records: [
        {
          id: 'model-1',
          name: 'Test',
          modelKind: 'schema',
          connectionId: null,
          baseModelId: null,
          createdAt: '2025-01-01T00:00:00Z',
          updatedAt: '2025-01-01T00:00:00Z',
          deletedAt: null,
        },
      ],
    }
    const result = PaginatedModelsResponseSchema.parse(data)
    expect(result.pageInfo.hasNextPage).toBe(true)
    expect(result.records).toHaveLength(1)
    expect(result.records[0].id).toBe('model-1')
  })
})

describe('ValidationResponseSchema', () => {
  it('parses a valid response', () => {
    const data = {
      valid: false,
      issues: [
        { level: 'error', message: 'Missing field', path: 'schema.columns' },
        { level: 'warning', message: 'Deprecated syntax' },
      ],
    }
    const result = ValidationResponseSchema.parse(data)
    expect(result.valid).toBe(false)
    expect(result.issues).toHaveLength(2)
  })

  it('issue with optional path field works with path', () => {
    const data = {
      valid: true,
      issues: [{ level: 'info', message: 'All good', path: 'root' }],
    }
    const result = ValidationResponseSchema.parse(data)
    expect(result.issues[0].path).toBe('root')
  })

  it('issue with optional path field works without path', () => {
    const data = {
      valid: true,
      issues: [{ level: 'info', message: 'All good' }],
    }
    const result = ValidationResponseSchema.parse(data)
    expect(result.issues[0].path).toBeUndefined()
  })
})
