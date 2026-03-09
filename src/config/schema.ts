import { z } from 'zod'

export const ProfileSchema = z.object({
  organizationId: z.string(),
  organizationShortId: z.string(),
  apiEndpoint: z.string().url(),
  authMethod: z.enum(['oauth', 'api-key']),
  apiKey: z.string().optional(),
})

export type Profile = z.infer<typeof ProfileSchema>

export const PreferencesSchema = z.object({
  defaultFormat: z.enum(['table', 'json', 'csv']).default('table'),
  colorOutput: z.boolean().default(true),
  maxTableWidth: z.number().default(120),
})

export type Preferences = z.infer<typeof PreferencesSchema>

export const ConfigSchema = z.object({
  version: z.literal(1),
  defaultProfile: z.string().optional(),
  profiles: z.record(z.string(), ProfileSchema),
  preferences: PreferencesSchema.optional(),
})

export type Config = z.infer<typeof ConfigSchema>

export const DEFAULT_CONFIG: Config = {
  version: 1,
  profiles: {},
  preferences: {
    defaultFormat: 'table',
    colorOutput: true,
    maxTableWidth: 120,
  },
}
