export { ConfigManager, getConfigManager } from './config-manager.js'
export {
  ConfigSchema,
  DEFAULT_CONFIG,
  PreferencesSchema,
  ProfileSchema,
} from './schema.js'
export type { Config, Preferences, Profile } from './schema.js'
export { getAuthContext, getAuthHeaders, validateAuth } from './auth.js'
export type { AuthContext } from './auth.js'
