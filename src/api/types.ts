import { z } from 'zod'

export const ModelMetadataSchema = z.object({
  id: z.string(),
  name: z.string().nullable(),
  modelKind: z.string().nullable(),
  connectionId: z.string().nullable(),
  baseModelId: z.string().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
  deletedAt: z.string().nullable(),
})

export type ModelMetadata = z.infer<typeof ModelMetadataSchema>

export const PageInfoSchema = z.object({
  hasNextPage: z.boolean(),
  nextCursor: z.string().nullable(),
  pageSize: z.number(),
  totalRecords: z.number(),
})

export const PaginatedModelsResponseSchema = z.object({
  pageInfo: PageInfoSchema,
  records: z.array(ModelMetadataSchema),
})

export type PaginatedModelsResponse = z.infer<
  typeof PaginatedModelsResponseSchema
>

export const ValidationIssueSchema = z.object({
  level: z.enum(['error', 'warning', 'info']),
  message: z.string(),
  path: z.string().optional(),
})

export type ValidationIssue = z.infer<typeof ValidationIssueSchema>

export const ValidationResponseSchema = z.object({
  issues: z.array(ValidationIssueSchema),
  valid: z.boolean(),
})

export type ValidationResponse = z.infer<typeof ValidationResponseSchema>

export const ModelYamlResponseSchema = z.object({
  files: z.record(z.string(), z.string()),
  version: z.string().optional(),
})

export type ModelYamlResponse = z.infer<typeof ModelYamlResponseSchema>
