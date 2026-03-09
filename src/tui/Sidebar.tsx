import React, { useState } from 'react'
import { Box, Text, useInput } from 'ink'
import { RETRO } from './theme.js'
import { useRouter } from './router.js'
import { usePaneFocus } from './focus.js'
import type { Section } from './router.js'

interface SidebarSection {
  id: Section
  label: string
}

const sections: SidebarSection[] = [
  { id: 'schedules', label: 'Schedules' },
  { id: 'models', label: 'Models' },
  { id: 'users', label: 'Users' },
  { id: 'config', label: 'Configuration' },
]

export const SIDEBAR_WIDTH = 20

export const Sidebar = () => {
  const { navigate, section } = useRouter()
  const { isSidebarActive, focusDetail } = usePaneFocus()

  const currentIndex = sections.findIndex((s) => s.id === section)
  const [cursor, setCursor] = useState(currentIndex >= 0 ? currentIndex : 0)

  useInput(
    (input, key) => {
      if (key.upArrow) {
        setCursor((c) => (c > 0 ? c - 1 : sections.length - 1))
        return
      }
      if (key.downArrow) {
        setCursor((c) => (c < sections.length - 1 ? c + 1 : 0))
        return
      }
      if (key.return || key.rightArrow) {
        navigate(sections[cursor].id)
        focusDetail()
        return
      }
    },
    { isActive: isSidebarActive }
  )

  return (
    <Box
      flexDirection="column"
      width={SIDEBAR_WIDTH}
      borderStyle={RETRO.border}
      borderColor={RETRO.colors.primary}
      paddingX={1}
    >
      <Box marginBottom={1}>
        <Text color={RETRO.colors.highlight} bold>
          {RETRO.title}
        </Text>
      </Box>
      {sections.map((s, i) => {
        const isCursor = i === cursor
        const isActive = s.id === section
        return (
          <Box key={s.id} gap={1}>
            <Text color={isCursor ? RETRO.colors.highlight : RETRO.colors.primary}>
              {isCursor ? RETRO.symbols.cursor : ' '}
            </Text>
            <Text
              color={isCursor ? RETRO.colors.highlight : isActive ? RETRO.colors.primary : RETRO.colors.dim}
              bold={isCursor}
            >
              {s.label}
            </Text>
          </Box>
        )
      })}
    </Box>
  )
}
