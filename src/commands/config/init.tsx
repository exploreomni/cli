import { Box, render, Text, useApp, useInput } from 'ink'
import TextInput from 'ink-text-input'
import type React from 'react'
import { useState } from 'react'
import { StatusMessage } from '../../components/index.js'
import { getConfigManager, type Profile } from '../../config/index.js'

type Step = 'name' | 'org' | 'endpoint' | 'auth' | 'apiKey' | 'done'

const ConfigInit: React.FC = () => {
  const { exit } = useApp()
  const configManager = getConfigManager()

  const [step, setStep] = useState<Step>('name')
  const [profileName, setProfileName] = useState('')
  const [orgId, setOrgId] = useState('')
  const [orgShortId, setOrgShortId] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [authMethod, setAuthMethod] = useState<'api-key' | 'oauth'>('api-key')
  const [apiKey, setApiKey] = useState('')
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = (value: string) => {
    setError(null)

    if (step === 'name') {
      if (!value.trim()) {
        setError('Profile name is required')
        return
      }
      if (configManager.getProfile(value)) {
        setError(`Profile '${value}' already exists`)
        return
      }
      setProfileName(value.trim())
      setStep('org')
    } else if (step === 'org') {
      if (!value.trim()) {
        setError('Organization ID is required')
        return
      }
      const parts = value.trim().split('/')
      if (parts.length === 1) {
        setOrgId(parts[0])
        setOrgShortId(parts[0])
      } else {
        setOrgShortId(parts[0])
        setOrgId(parts[1])
      }
      setStep('endpoint')
    } else if (step === 'endpoint') {
      let url = value.trim()
      if (!url) {
        url = `https://${orgShortId}.omniapp.co`
      }
      if (!url.startsWith('http')) {
        url = `https://${url}`
      }
      setEndpoint(url)
      setStep('auth')
    } else if (step === 'apiKey') {
      const profile: Profile = {
        organizationId: orgId,
        organizationShortId: orgShortId,
        apiEndpoint: endpoint,
        authMethod,
        ...(value.trim() ? { apiKey: value.trim() } : {}),
      }
      configManager.setProfile(profileName, profile)
      setStep('done')
      setTimeout(() => exit(), 100)
    }
  }

  useInput((input) => {
    if (step === 'auth') {
      if (input === '1' || input.toLowerCase() === 'a') {
        setAuthMethod('api-key')
        setStep('apiKey')
      } else if (input === '2' || input.toLowerCase() === 'o') {
        setAuthMethod('oauth')
        const profile: Profile = {
          organizationId: orgId,
          organizationShortId: orgShortId,
          apiEndpoint: endpoint,
          authMethod: 'oauth',
        }
        configManager.setProfile(profileName, profile)
        setStep('done')
        setTimeout(() => exit(), 100)
      }
    }
  })

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Configure Omni CLI Profile</Text>

      {step === 'name' && (
        <Box flexDirection="column">
          <Text>Profile name:</Text>
          <Box>
            <Text color="cyan">&gt; </Text>
            <TextInput
              value={profileName}
              onChange={setProfileName}
              onSubmit={handleSubmit}
              placeholder="production"
            />
          </Box>
        </Box>
      )}

      {step === 'org' && (
        <Box flexDirection="column">
          <StatusMessage status="success">Profile: {profileName}</StatusMessage>
          <Text>
            Organization ID or short ID (e.g., myorg or myorg/org_abc123):
          </Text>
          <Box>
            <Text color="cyan">&gt; </Text>
            <TextInput
              value={orgId}
              onChange={setOrgId}
              onSubmit={handleSubmit}
              placeholder="myorg"
            />
          </Box>
        </Box>
      )}

      {step === 'endpoint' && (
        <Box flexDirection="column">
          <StatusMessage status="success">Profile: {profileName}</StatusMessage>
          <StatusMessage status="success">
            Organization: {orgShortId}
          </StatusMessage>
          <Text>API endpoint (press Enter for default):</Text>
          <Box>
            <Text color="cyan">&gt; </Text>
            <TextInput
              value={endpoint}
              onChange={setEndpoint}
              onSubmit={handleSubmit}
              placeholder={`https://${orgShortId}.omniapp.co`}
            />
          </Box>
        </Box>
      )}

      {step === 'auth' && (
        <Box flexDirection="column">
          <StatusMessage status="success">Profile: {profileName}</StatusMessage>
          <StatusMessage status="success">
            Organization: {orgShortId}
          </StatusMessage>
          <StatusMessage status="success">Endpoint: {endpoint}</StatusMessage>
          <Text>Authentication method:</Text>
          <Text> [1] API Key</Text>
          <Text> [2] OAuth (browser login)</Text>
          <Text dimColor>Press 1 or 2 to select</Text>
        </Box>
      )}

      {step === 'apiKey' && (
        <Box flexDirection="column">
          <StatusMessage status="success">Profile: {profileName}</StatusMessage>
          <StatusMessage status="success">
            Organization: {orgShortId}
          </StatusMessage>
          <StatusMessage status="success">Endpoint: {endpoint}</StatusMessage>
          <StatusMessage status="success">Auth: API Key</StatusMessage>
          <Text>API Key (or press Enter to skip, set OMNI_API_KEY later):</Text>
          <Box>
            <Text color="cyan">&gt; </Text>
            <TextInput
              value={apiKey}
              onChange={setApiKey}
              onSubmit={handleSubmit}
              placeholder="omni_osk_..."
              mask="*"
            />
          </Box>
        </Box>
      )}

      {step === 'done' && (
        <Box flexDirection="column">
          <StatusMessage status="success">
            Profile '{profileName}' created successfully!
          </StatusMessage>
          <Text dimColor>Config saved to: {configManager.configPath}</Text>
        </Box>
      )}

      {error && <StatusMessage status="error">{error}</StatusMessage>}
    </Box>
  )
}

export const runConfigInit = (): void => {
  render(<ConfigInit />)
}
