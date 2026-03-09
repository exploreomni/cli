import React, { useState, useCallback, useMemo } from 'react'
import { Box, Text, useApp, useInput } from 'ink'
import {
  SelectableList,
  ActionBar,
  ResourceFrame,
} from '../components/index.js'
import type { ListItem } from '../components/index.js'
import { useRouter } from '../router.js'
import { usePaneFocus } from '../focus.js'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { RETRO } from '../theme.js'
import {
  executeUserList,
  formatUserRow,
} from '../../commands/user/list.execute.js'

export const UserListView = () => {
  const { push } = useRouter()
  const { exit } = useApp()
  const { isDetailActive, focusSidebar } = usePaneFocus()
  const [searchQuery, setSearchQuery] = useState('')

  const { data, loading, error } = useAsyncData(() =>
    executeUserList({}).then((r) => r.data)
  )

  const filteredUsers = useMemo(() => {
    if (!data?.users) return []
    if (!searchQuery) return data.users
    const q = searchQuery.toLowerCase()
    return data.users.filter(
      (u) =>
        u.displayName.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q)
    )
  }, [data, searchQuery])

  const handleSelect = useCallback(
    (item: ListItem) => {
      const user = filteredUsers.find((u) => u.id === item.id)
      if (user) push({ view: 'user-detail', userId: user.id, user })
    },
    [push, filteredUsers]
  )

  useInput(
    (input, key) => {
      if (key.escape) {
        if (searchQuery) {
          setSearchQuery('')
          return
        }
        focusSidebar()
        return
      }
      if (input === 'q' && !searchQuery) {
        exit()
        return
      }
      if (key.backspace || key.delete) {
        setSearchQuery((q) => q.slice(0, -1))
        return
      }
      if (input && !key.upArrow && !key.downArrow && !key.return && !key.tab) {
        setSearchQuery((q) => q + input)
      }
    },
    { isActive: isDetailActive }
  )

  const items: ListItem[] = useMemo(() => {
    return filteredUsers.map((u) => {
      const row = formatUserRow(u)
      return {
        id: u.id,
        label: row.name,
        columns: [row.email, row.active],
      }
    })
  }, [filteredUsers])

  const footer = (
    <Box flexDirection="column" gap={0}>
      {searchQuery && (
        <Text color={RETRO.colors.highlight}>
          Search: {searchQuery}
        </Text>
      )}
      <ActionBar
        actions={[
          { key: 'Type', label: 'Search' },
          { key: 'Enter', label: 'Detail' },
          { key: 'Esc', label: searchQuery ? 'Clear' : 'Menu' },
          ...(!searchQuery ? [{ key: 'q', label: 'Quit' }] : []),
        ]}
      />
    </Box>
  )

  return (
    <ResourceFrame
      title="USERS"
      loading={loading}
      error={error}
      count={filteredUsers.length}
      loadingLabel="Loading users..."
      borderless
      footer={footer}
    >
      <SelectableList
        items={items}
        onSelect={handleSelect}
        onBack={() => {
          if (searchQuery) {
            setSearchQuery('')
          } else {
            focusSidebar()
          }
        }}
        active={isDetailActive}
      />
    </ResourceFrame>
  )
}
