import { Text, useApp, useInput } from 'ink'
import type { UserListItem } from '../../api/index.js'
import { ActionBar, KeyValueFields, RetroFrame } from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { useRouter } from '../router.js'
import { RETRO } from '../theme.js'

export const UserDetailView = ({
  userId: _userId,
  user,
}: {
  userId: string
  user?: UserListItem
}) => {
  const { pop } = useRouter()
  const { exit } = useApp()
  const { isDetailActive } = usePaneFocus()

  useInput(
    (input, key) => {
      if (key.escape) {
        pop()
        return
      }
      if (input === 'q') {
        exit()
        return
      }
    },
    { isActive: isDetailActive }
  )

  if (!user) {
    return (
      <RetroFrame title="USER DETAIL" borderless>
        <Text color={RETRO.colors.error}>User not found</Text>
      </RetroFrame>
    )
  }

  const formatLastLogin = (lastLogin: string | null): string => {
    if (!lastLogin) return '-'
    const date = new Date(lastLogin)
    return date.toLocaleString()
  }

  const fields: [string, string][] = [
    ['Name', user.displayName],
    ['Email', user.email],
    ['Active', user.active ? 'yes' : 'no'],
    ['Groups', user.groups.map((g) => g.display).join(', ') || '-'],
    ['Last Login', formatLastLogin(user.lastLogin)],
    ['Membership ID', user.id],
  ]

  return (
    <RetroFrame
      title={`USER: ${user.displayName}`}
      borderless
      footer={
        <ActionBar
          actions={[
            { key: 'Esc', label: 'Back' },
            { key: 'q', label: 'Quit' },
          ]}
        />
      }
    >
      <KeyValueFields fields={fields} />
    </RetroFrame>
  )
}
