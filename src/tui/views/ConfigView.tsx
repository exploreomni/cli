import React from 'react'
import { Box, Text, useApp, useInput } from 'ink'
import { RetroFrame, ActionBar } from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { RETRO } from '../theme.js'
import { executeConfigShow } from '../../commands/config/show.execute.js'

export const ConfigView = () => {
  const { exit } = useApp()
  const { isDetailActive, focusSidebar } = usePaneFocus()

  useInput(
    (input, key) => {
      if (key.escape) {
        focusSidebar()
        return
      }
      if (input === 'q') exit()
    },
    { isActive: isDetailActive }
  )

  const result = executeConfigShow()
  const { profiles, configPath } = result.data

  return (
    <RetroFrame
      title="CONFIGURATION"
      borderless
      footer={
        <ActionBar
          actions={[
            { key: 'Esc', label: 'Menu' },
            { key: 'q', label: 'Quit' },
          ]}
        />
      }
    >
      <Box flexDirection="column">
        <Box marginBottom={1}>
          <Text color={RETRO.colors.dim}>Config: {configPath}</Text>
        </Box>

        {profiles.length === 0 ? (
          <Text color={RETRO.colors.dim} italic>
            No profiles configured. Run omni-cli config:init
          </Text>
        ) : (
          profiles.map((profile) => (
            <Box key={profile.name} flexDirection="column" marginBottom={1}>
              <Box gap={1}>
                <Text color={RETRO.colors.highlight} bold>
                  {profile.isDefault ? RETRO.symbols.cursor : ' '}{' '}
                  {profile.name}
                </Text>
                {profile.isDefault && (
                  <Text color={RETRO.colors.dim}>(default)</Text>
                )}
              </Box>
              <Box paddingLeft={4} flexDirection="column">
                <Box gap={1}>
                  <Text color={RETRO.colors.dim}>{'Org'.padEnd(10)}</Text>
                  <Text color={RETRO.colors.primary}>
                    {profile.organization}
                  </Text>
                </Box>
                <Box gap={1}>
                  <Text color={RETRO.colors.dim}>{'Endpoint'.padEnd(10)}</Text>
                  <Text color={RETRO.colors.primary}>{profile.endpoint}</Text>
                </Box>
                <Box gap={1}>
                  <Text color={RETRO.colors.dim}>{'Auth'.padEnd(10)}</Text>
                  <Text color={RETRO.colors.primary}>{profile.auth}</Text>
                </Box>
              </Box>
            </Box>
          ))
        )}
      </Box>
    </RetroFrame>
  )
}
