import React from 'react'
import { Text } from 'ink'
import { RetroFrame } from './RetroFrame.js'
import { RETRO } from '../theme.js'
import { Spinner } from '../../components/index.js'

interface ResourceFrameProps {
  title: string
  loading: boolean
  error: string | null
  count?: number
  loadingLabel?: string
  footer?: React.ReactNode
  borderless?: boolean
  children: React.ReactNode
}

export const ResourceFrame = ({
  title,
  loading,
  error,
  count,
  loadingLabel,
  footer,
  borderless,
  children,
}: ResourceFrameProps) => {
  if (loading) {
    return (
      <RetroFrame title={title} borderless={borderless}>
        <Spinner label={loadingLabel ?? 'Loading...'} />
      </RetroFrame>
    )
  }

  if (error) {
    return (
      <RetroFrame title={title} borderless={borderless}>
        <Text color={RETRO.colors.error}>Error: {error}</Text>
      </RetroFrame>
    )
  }

  const displayTitle = count != null ? `${title} (${count})` : title

  return (
    <RetroFrame title={displayTitle} footer={footer} borderless={borderless}>
      {children}
    </RetroFrame>
  )
}
