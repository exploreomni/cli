import { Box, Text } from 'ink'
import InkSpinner from 'ink-spinner'
import type React from 'react'

interface SpinnerProps {
  label?: string
}

export const Spinner: React.FC<SpinnerProps> = ({ label }) => {
  return (
    <Box>
      <Text color="cyan">
        <InkSpinner type="dots" />
      </Text>
      {label && <Text> {label}</Text>}
    </Box>
  )
}
