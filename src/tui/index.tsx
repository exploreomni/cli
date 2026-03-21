import { render } from 'ink'
import { FocusPaneProvider } from './focus.js'
import { RouterProvider, useRouter } from './router.js'
import { ShellLayout } from './ShellLayout.js'
import {
  ConfigView,
  ModelDetailView,
  ModelListView,
  ScheduleDetailView,
  ScheduleListView,
  UserDetailView,
  UserListView,
} from './views/index.js'

const ViewRouter = () => {
  const { current } = useRouter()

  switch (current.view) {
    case 'schedule-list':
      return <ScheduleListView />
    case 'schedule-detail':
      return (
        <ScheduleDetailView
          scheduleId={current.scheduleId}
          schedule={current.schedule}
        />
      )
    case 'model-list':
      return <ModelListView />
    case 'model-detail':
      return <ModelDetailView modelId={current.modelId} />
    case 'config':
      return <ConfigView />
    case 'user-list':
      return <UserListView />
    case 'user-detail':
      return <UserDetailView userId={current.userId} user={current.user} />
  }
}

const InteractiveApp = () => (
  <FocusPaneProvider>
    <RouterProvider>
      <ShellLayout>
        <ViewRouter />
      </ShellLayout>
    </RouterProvider>
  </FocusPaneProvider>
)

export const runTui = () => {
  process.stdout.write('\x1b[?1049h\x1b[?25l')

  const instance = render(<InteractiveApp />)

  const cleanup = () => {
    process.stdout.write('\x1b[?25h\x1b[?1049l')
  }

  instance.waitUntilExit().then(cleanup)
  process.on('exit', cleanup)
}
