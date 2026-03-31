import { Box, useApp, useInput } from 'ink'
import type React from 'react'
import { usePaneFocus } from './focus.js'
import { useTerminalSize } from './hooks/useTerminalSize.js'
import { Sidebar } from './Sidebar.js'
import { RETRO } from './theme.js'

export const ShellLayout = ({ children }: { children: React.ReactNode }) => {
  const { columns, rows } = useTerminalSize()
  const { isDetailActive, focusSidebar } = usePaneFocus()
  const { exit } = useApp()

  useInput((input, key) => {
    if (key.leftArrow && isDetailActive) {
      focusSidebar()
      return
    }
    if (input === 'q' && !isDetailActive) {
      exit()
    }
  })

  return (
    <Box width={columns} height={rows} flexDirection="row" overflow="hidden">
      <Sidebar />
      <Box
        flexDirection="column"
        flexGrow={1}
        borderStyle={RETRO.border}
        borderColor={RETRO.colors.primary}
        borderLeft={false}
        paddingX={1}
      >
        {children}
      </Box>
    </Box>
  )
}
