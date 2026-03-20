import { Box, Text, useInput } from 'ink'
import { useState } from 'react'
import { RETRO } from '../theme.js'

export interface ListItem {
  id: string
  label: string
  columns?: string[]
}

interface SelectableListProps {
  items: ListItem[]
  multiSelect?: boolean
  selectedIds?: Set<string>
  onToggle?: (id: string) => void
  onToggleAll?: () => void
  onSelect: (item: ListItem) => void
  onBack?: () => void
  extraKeys?: (input: string, key: { escape: boolean }) => void
  active?: boolean
}

export const SelectableList = ({
  items,
  multiSelect = false,
  selectedIds,
  onToggle,
  onToggleAll,
  onSelect,
  onBack,
  extraKeys,
  active = true,
}: SelectableListProps) => {
  const [cursor, setCursor] = useState(0)

  useInput(
    (input, key) => {
      if (!active) return

      if (key.upArrow) {
        setCursor((c) => (c > 0 ? c - 1 : items.length - 1))
        return
      }
      if (key.downArrow) {
        setCursor((c) => (c < items.length - 1 ? c + 1 : 0))
        return
      }
      if (key.return && items[cursor]) {
        onSelect(items[cursor])
        return
      }
      if (key.escape && onBack) {
        onBack()
        return
      }
      if (input === ' ' && multiSelect && items[cursor] && onToggle) {
        onToggle(items[cursor].id)
        return
      }
      if (input === 'a' && multiSelect && onToggleAll) {
        onToggleAll()
        return
      }

      extraKeys?.(input, key)
    },
    { isActive: active }
  )

  if (items.length === 0) {
    return (
      <Text color={RETRO.colors.dim} italic>
        (empty)
      </Text>
    )
  }

  return (
    <Box flexDirection="column">
      {items.map((item, i) => {
        const isCursor = i === cursor
        const isSelected = selectedIds?.has(item.id) ?? false

        return (
          <Box key={item.id} gap={1}>
            {multiSelect && (
              <Text
                color={isCursor ? RETRO.colors.highlight : RETRO.colors.primary}
              >
                {isSelected ? RETRO.symbols.checked : RETRO.symbols.unchecked}
              </Text>
            )}
            <Text
              color={isCursor ? RETRO.colors.highlight : RETRO.colors.primary}
            >
              {isCursor ? RETRO.symbols.cursor : ' '}
            </Text>
            <Text
              color={isCursor ? RETRO.colors.highlight : RETRO.colors.primary}
              bold={isCursor}
            >
              {item.label}
            </Text>
            {item.columns?.map((col, ci) => (
              <Text
                key={ci}
                color={isCursor ? RETRO.colors.highlight : RETRO.colors.dim}
              >
                {col}
              </Text>
            ))}
          </Box>
        )
      })}
    </Box>
  )
}
