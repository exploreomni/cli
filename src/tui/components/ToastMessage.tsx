import React, { useEffect, useState } from 'react'
import { Text } from 'ink'
import { RETRO } from '../theme.js'

interface ToastMessageProps {
  message: string
  type?: 'success' | 'error' | 'info'
  durationMs?: number
  onDismiss?: () => void
}

const toastColors = {
  success: RETRO.colors.success,
  error: RETRO.colors.error,
  info: RETRO.colors.highlight,
} as const

export const ToastMessage = ({
  message,
  type = 'info',
  durationMs = 3000,
  onDismiss,
}: ToastMessageProps) => {
  const [visible, setVisible] = useState(true)

  useEffect(() => {
    const timer = setTimeout(() => {
      setVisible(false)
      onDismiss?.()
    }, durationMs)
    return () => clearTimeout(timer)
  }, [durationMs, onDismiss])

  if (!visible) return null

  return (
    <Text color={toastColors[type]} bold>
      {message}
    </Text>
  )
}
