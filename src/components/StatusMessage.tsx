import React from 'react'
import { Box, Text } from 'ink'

type Status = 'success' | 'error' | 'warning' | 'info'

interface StatusMessageProps {
  status: Status
  children: React.ReactNode
}

const statusConfig: Record<Status, { symbol: string; color: string }> = {
  success: { symbol: '✓', color: 'green' },
  error: { symbol: '✗', color: 'red' },
  warning: { symbol: '⚠', color: 'yellow' },
  info: { symbol: 'ℹ', color: 'blue' },
}

export const StatusMessage: React.FC<StatusMessageProps> = ({
  status,
  children,
}) => {
  const config = statusConfig[status]
  return (
    <Box>
      <Text color={config.color}>{config.symbol}</Text>
      <Text> {children}</Text>
    </Box>
  )
}
