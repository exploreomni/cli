import { useApp, useInput } from 'ink'
import type { ListItem } from '../components/index.js'
import { ActionBar, RetroFrame, SelectableList } from '../components/index.js'
import type { Route } from '../router.js'
import { useRouter } from '../router.js'
import { RETRO } from '../theme.js'

const menuItems: (ListItem & { route: Route })[] = [
  { id: 'schedules', label: 'Schedules', route: { view: 'schedule-list' } },
  { id: 'models', label: 'Models', route: { view: 'model-list' } },
  { id: 'users', label: 'Users', route: { view: 'user-list' } },
  { id: 'config', label: 'Configuration', route: { view: 'config' } },
]

export const MainMenuView = () => {
  const { push } = useRouter()
  const { exit } = useApp()

  useInput((input) => {
    if (input === 'q') exit()
  })

  const handleSelect = (item: ListItem) => {
    const menuItem = menuItems.find((m) => m.id === item.id)
    if (menuItem) push(menuItem.route)
  }

  return (
    <RetroFrame
      title={RETRO.title}
      footer={
        <ActionBar
          actions={[
            { key: 'Enter', label: 'Select' },
            { key: 'q', label: 'Quit' },
          ]}
        />
      }
    >
      <SelectableList items={menuItems} onSelect={handleSelect} />
    </RetroFrame>
  )
}
