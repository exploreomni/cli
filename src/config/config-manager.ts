import Conf from 'conf'
import { Config, ConfigSchema, DEFAULT_CONFIG, Profile } from './schema.js'

const CONFIG_NAME = 'omni-cli'

export class ConfigManager {
  private store: Conf<Config>

  constructor() {
    this.store = new Conf<Config>({
      projectName: CONFIG_NAME,
      defaults: DEFAULT_CONFIG,
      schema: {
        version: { type: 'number' },
        defaultProfile: { type: 'string' },
        profiles: { type: 'object' },
        preferences: { type: 'object' },
      },
    })
  }

  get configPath(): string {
    return this.store.path
  }

  getConfig(): Config {
    const raw = this.store.store
    const result = ConfigSchema.safeParse(raw)
    if (!result.success) {
      return DEFAULT_CONFIG
    }
    return result.data
  }

  getDefaultProfile(): string | undefined {
    return this.store.get('defaultProfile')
  }

  setDefaultProfile(name: string): void {
    const profiles = this.store.get('profiles')
    if (!profiles[name]) {
      throw new Error(`Profile '${name}' does not exist`)
    }
    this.store.set('defaultProfile', name)
  }

  getProfile(name?: string): Profile | undefined {
    const profileName = name ?? this.getDefaultProfile()
    if (!profileName) {
      return undefined
    }
    const profiles = this.store.get('profiles')
    return profiles[profileName]
  }

  getProfiles(): Record<string, Profile> {
    return this.store.get('profiles') ?? {}
  }

  setProfile(name: string, profile: Profile): void {
    const profiles = this.store.get('profiles') ?? {}
    profiles[name] = profile
    this.store.set('profiles', profiles)

    if (!this.getDefaultProfile()) {
      this.store.set('defaultProfile', name)
    }
  }

  deleteProfile(name: string): void {
    const profiles = this.store.get('profiles') ?? {}
    delete profiles[name]
    this.store.set('profiles', profiles)

    if (this.getDefaultProfile() === name) {
      const remainingNames = Object.keys(profiles)
      if (remainingNames.length > 0) {
        this.store.set('defaultProfile', remainingNames[0])
      } else {
        this.store.delete('defaultProfile')
      }
    }
  }

  clear(): void {
    this.store.clear()
  }
}

let configManager: ConfigManager | null = null

export const getConfigManager = (): ConfigManager => {
  if (!configManager) {
    configManager = new ConfigManager()
  }
  return configManager
}
