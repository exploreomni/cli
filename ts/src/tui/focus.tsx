import type React from 'react'
import { createContext, useCallback, useContext, useState } from 'react'

type Pane = 'sidebar' | 'detail'

interface PaneFocusState {
  activePane: Pane
  isSidebarActive: boolean
  isDetailActive: boolean
  focusSidebar: () => void
  focusDetail: () => void
}

const FocusPaneContext = createContext<PaneFocusState | null>(null)

export const usePaneFocus = (): PaneFocusState => {
  const ctx = useContext(FocusPaneContext)
  if (!ctx)
    throw new Error('usePaneFocus must be used within FocusPaneProvider')
  return ctx
}

export const FocusPaneProvider = ({
  children,
}: {
  children: React.ReactNode
}) => {
  const [activePane, setActivePane] = useState<Pane>('sidebar')

  const focusSidebar = useCallback(() => setActivePane('sidebar'), [])
  const focusDetail = useCallback(() => setActivePane('detail'), [])

  return (
    <FocusPaneContext.Provider
      value={{
        activePane,
        isSidebarActive: activePane === 'sidebar',
        isDetailActive: activePane === 'detail',
        focusSidebar,
        focusDetail,
      }}
    >
      {children}
    </FocusPaneContext.Provider>
  )
}
