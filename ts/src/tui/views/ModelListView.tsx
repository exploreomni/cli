import { useApp, useInput } from 'ink'
import { useCallback, useMemo } from 'react'
import { executeModelList } from '../../commands/model/list.execute.js'
import type { ListItem } from '../components/index.js'
import {
  ActionBar,
  ResourceFrame,
  SelectableList,
} from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { useRouter } from '../router.js'

export const ModelListView = () => {
  const { push } = useRouter()
  const { exit } = useApp()
  const { isDetailActive, focusSidebar } = usePaneFocus()

  const { data, loading, error } = useAsyncData(() =>
    executeModelList({}).then((r) => r.data)
  )

  useInput(
    (input, key) => {
      if (key.escape) {
        focusSidebar()
        return
      }
      if (input === 'q') exit()
    },
    { isActive: isDetailActive }
  )

  const handleSelect = useCallback(
    (item: ListItem) => {
      push({ view: 'model-detail', modelId: item.id })
    },
    [push]
  )

  const items: ListItem[] = useMemo(() => {
    if (!data) return []
    return data.models.map((m) => ({
      id: m.raw.id,
      label: m.name,
      columns: [m.kind, m.updated],
    }))
  }, [data])

  return (
    <ResourceFrame
      title="MODELS"
      loading={loading}
      error={error}
      count={data?.count}
      loadingLabel="Loading models..."
      borderless
      footer={
        <ActionBar
          actions={[
            { key: 'Enter', label: 'Detail' },
            { key: 'Esc', label: 'Menu' },
            { key: 'q', label: 'Quit' },
          ]}
        />
      }
    >
      <SelectableList
        items={items}
        onSelect={handleSelect}
        onBack={focusSidebar}
        active={isDetailActive}
      />
    </ResourceFrame>
  )
}
