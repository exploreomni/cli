import React from 'react'
import { Box, Text } from 'ink'
import { RETRO } from '../theme.js'

interface Action {
  key: string
  label: string
}

interface ActionBarProps {
  actions: Action[]
  prefix?: string
}

export const ActionBar = ({ actions, prefix }: ActionBarProps) => (
  <Box gap={1} flexWrap="wrap">
    {prefix && (
      <Text color={RETRO.colors.highlight}>{prefix}</Text>
    )}
    {actions.map((action) => (
      <Text key={action.key} color={RETRO.colors.dim}>
        <Text color={RETRO.colors.highlight}>[{action.key}]</Text> {action.label}
      </Text>
    ))}
  </Box>
)
