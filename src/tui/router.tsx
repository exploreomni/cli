import type React from 'react'
import { createContext, useCallback, useContext, useState } from 'react'
import type { ScheduleListItem, UserListItem } from '../api/index.js'

export type Section = 'schedules' | 'models' | 'users' | 'config'

export type Route =
  | { view: 'schedule-list' }
  | { view: 'schedule-detail'; scheduleId: string; schedule?: ScheduleListItem }
  | { view: 'model-list' }
  | { view: 'model-detail'; modelId: string }
  | { view: 'config' }
  | { view: 'user-list' }
  | { view: 'user-detail'; userId: string; user?: UserListItem }

const sectionRootRoutes: Record<Section, Route> = {
  schedules: { view: 'schedule-list' },
  models: { view: 'model-list' },
  users: { view: 'user-list' },
  config: { view: 'config' },
}

export const routeToSection = (route: Route): Section => {
  switch (route.view) {
    case 'schedule-list':
    case 'schedule-detail':
      return 'schedules'
    case 'model-list':
    case 'model-detail':
      return 'models'
    case 'user-list':
    case 'user-detail':
      return 'users'
    case 'config':
      return 'config'
  }
}

interface RouterState {
  stack: Route[]
  push: (route: Route) => void
  pop: () => void
  navigate: (section: Section) => void
  current: Route
  section: Section
  isAtSectionRoot: boolean
}

const RouterContext = createContext<RouterState | null>(null)

export const useRouter = (): RouterState => {
  const ctx = useContext(RouterContext)
  if (!ctx) throw new Error('useRouter must be used within RouterProvider')
  return ctx
}

export const RouterProvider = ({ children }: { children: React.ReactNode }) => {
  const [stack, setStack] = useState<Route[]>([{ view: 'schedule-list' }])

  const push = useCallback((route: Route) => {
    setStack((prev) => [...prev, route])
  }, [])

  const pop = useCallback(() => {
    setStack((prev) => (prev.length > 1 ? prev.slice(0, -1) : prev))
  }, [])

  const navigate = useCallback((section: Section) => {
    setStack([sectionRootRoutes[section]])
  }, [])

  const current = stack[stack.length - 1]
  const section = routeToSection(current)
  const isAtSectionRoot = stack.length === 1

  return (
    <RouterContext.Provider
      value={{ stack, push, pop, navigate, current, section, isAtSectionRoot }}
    >
      {children}
    </RouterContext.Provider>
  )
}
