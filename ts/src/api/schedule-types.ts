import { z } from 'zod'

export const ScheduleListItemSchema = z.object({
  id: z.string(),
  name: z.string(),
  dashboardName: z.string(),
  ownerName: z.string(),
  ownerId: z.string(),
  destinationType: z.string(),
  format: z.string(),
  schedule: z.string(),
  timezone: z.string(),
  content: z.string(),
  identifier: z.string(),
  disabledAt: z.string().nullable(),
  lastStatus: z.string().nullable(),
  lastCompletedAt: z.string().nullable(),
  recipientCount: z.number(),
  systemDisabledAt: z.string().nullable(),
  systemDisabledReason: z.string().nullable(),
  slackRecipientType: z.string().nullable(),
  alert: z
    .object({
      conditionQueryName: z.string().nullable(),
      conditionType: z.string(),
    })
    .optional(),
})

export type ScheduleListItem = z.infer<typeof ScheduleListItemSchema>

export const PageInfoSchema = z.object({
  hasNextPage: z.boolean(),
  nextCursor: z.string().nullable().optional(),
  pageSize: z.number(),
  totalRecords: z.number(),
})

export const ScheduleListResponseSchema = z.object({
  pageInfo: PageInfoSchema,
  records: z.array(ScheduleListItemSchema),
})

export type ScheduleListResponse = z.infer<typeof ScheduleListResponseSchema>

export const SuccessResponseSchema = z.object({
  success: z.boolean(),
})

export type SuccessResponse = z.infer<typeof SuccessResponseSchema>

export const EmailRecipientSchema = z.object({
  id: z.string(),
  name: z.string(),
  email: z.string(),
})

export type EmailRecipient = z.infer<typeof EmailRecipientSchema>

export const UserGroupRecipientSchema = z.object({
  id: z.string(),
  name: z.string(),
  recipients: z.array(EmailRecipientSchema),
})

export type UserGroupRecipient = z.infer<typeof UserGroupRecipientSchema>

export const RecipientsResponseSchema = z.object({
  type: z.string(),
  recipients: z.array(EmailRecipientSchema).optional(),
  userGroupRecipients: z.array(UserGroupRecipientSchema).optional(),
})

export type RecipientsResponse = z.infer<typeof RecipientsResponseSchema>
