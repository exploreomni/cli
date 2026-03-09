import { z } from 'zod'

export const UserListItemSchema = z.object({
  id: z.string(),
  displayName: z.string(),
  email: z.string(),
  active: z.boolean(),
  groups: z.array(z.object({ display: z.string(), value: z.string() })),
  lastLogin: z.string().nullable(),
})

export type UserListItem = z.infer<typeof UserListItemSchema>

export const UserListResponseSchema = z.object({
  users: z.array(UserListItemSchema),
  totalResults: z.number(),
  startIndex: z.number(),
  itemsPerPage: z.number(),
})

export type UserListResponse = z.infer<typeof UserListResponseSchema>
