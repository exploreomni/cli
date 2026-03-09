import React from 'react'
import { Box, Text, useInput } from 'ink'
import { RETRO } from '../theme.js'

interface ConfirmDialogProps {
  message: string
  onConfirm: () => void
  onCancel: () => void
  active?: boolean
}

export const ConfirmDialog = ({
  message,
  onConfirm,
  onCancel,
  active = true,
}: ConfirmDialogProps) => {
  useInput(
    (input) => {
      if (input === 'y' || input === 'Y') onConfirm()
      if (input === 'n' || input === 'N' || input === 'q') onCancel()
    },
    { isActive: active }
  )

  return (
    <Box
      borderStyle={RETRO.border}
      borderColor={RETRO.colors.warning}
      paddingX={2}
      paddingY={1}
      flexDirection="column"
    >
      <Text color={RETRO.colors.warning} bold>
        {message}
      </Text>
      <Box marginTop={1}>
        <Text color={RETRO.colors.dim}>
          <Text color={RETRO.colors.highlight}>[y]</Text> Yes{' '}
          <Text color={RETRO.colors.highlight}>[n]</Text> No
        </Text>
      </Box>
    </Box>
  )
}
