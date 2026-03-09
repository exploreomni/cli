import React from 'react'
import { Box, Text } from 'ink'
import { RETRO } from '../theme.js'

interface RetroFrameProps {
  title: string
  footer?: React.ReactNode
  borderless?: boolean
  children: React.ReactNode
}

export const RetroFrame = ({ title, footer, borderless, children }: RetroFrameProps) => (
  <Box flexDirection="column">
    <Box
      borderStyle={borderless ? undefined : RETRO.border}
      borderColor={borderless ? undefined : RETRO.colors.primary}
      flexDirection="column"
      paddingX={borderless ? 0 : 1}
    >
      <Box marginBottom={1}>
        <Text color={RETRO.colors.highlight} bold>
          {title}
        </Text>
      </Box>
      {children}
    </Box>
    {footer && (
      <Box
        borderStyle={borderless ? undefined : 'single'}
        borderColor={borderless ? undefined : RETRO.colors.dim}
        paddingX={borderless ? 0 : 1}
      >
        {footer}
      </Box>
    )}
  </Box>
)
