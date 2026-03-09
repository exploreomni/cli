import React from 'react'
import { Box, Text } from 'ink'
import { RETRO } from '../theme.js'

interface KeyValueFieldsProps {
  fields: [string, string][]
  labelWidth?: number
}

export const KeyValueFields = ({ fields, labelWidth = 14 }: KeyValueFieldsProps) => (
  <Box flexDirection="column">
    {fields.map(([label, value]) => (
      <Box key={label} gap={1}>
        <Text color={RETRO.colors.dim}>{label.padEnd(labelWidth)}</Text>
        <Text color={RETRO.colors.primary}>{value}</Text>
      </Box>
    ))}
  </Box>
)
