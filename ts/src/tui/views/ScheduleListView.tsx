import { Box, Text, useApp } from 'ink'
import { useCallback, useMemo, useState } from 'react'
import { executeScheduleDelete } from '../../commands/schedule/delete.execute.js'
import {
  executeScheduleList,
  formatScheduleRow,
} from '../../commands/schedule/list.execute.js'
import { executeSchedulePause } from '../../commands/schedule/pause.execute.js'
import { executeScheduleResume } from '../../commands/schedule/resume.execute.js'
import { executeScheduleTrigger } from '../../commands/schedule/trigger.execute.js'
import type { ListItem } from '../components/index.js'
import {
  ActionBar,
  ConfirmDialog,
  ResourceFrame,
  SelectableList,
  ToastMessage,
} from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { useBulkAction } from '../hooks/useBulkAction.js'
import { useRouter } from '../router.js'
import { RETRO } from '../theme.js'

type PendingAction = 'delete' | 'pause' | 'resume' | 'trigger'

const actionLabels: Record<PendingAction, string> = {
  delete: 'Delete',
  pause: 'Pause',
  resume: 'Resume',
  trigger: 'Trigger',
}

export const ScheduleListView = () => {
  const { push } = useRouter()
  const { exit } = useApp()
  const { isDetailActive, focusSidebar } = usePaneFocus()
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [toast, setToast] = useState<{
    message: string
    type: 'success' | 'error'
  } | null>(null)

  const { data, loading, error, reload } = useAsyncData(() =>
    executeScheduleList({}).then((r) => r.data)
  )

  const actionExecutors: Record<
    PendingAction,
    (id: string) => Promise<unknown>
  > = useMemo(
    () => ({
      delete: (id) => executeScheduleDelete({ scheduleId: id }),
      pause: (id) => executeSchedulePause({ scheduleId: id }),
      resume: (id) => executeScheduleResume({ scheduleId: id }),
      trigger: (id) => executeScheduleTrigger({ scheduleId: id }),
    }),
    []
  )

  const onBulkComplete = useCallback(() => {
    setSelectedIds(new Set())
    reload()
  }, [reload])

  const bulkDelete = useBulkAction(actionExecutors.delete, onBulkComplete)
  const bulkPause = useBulkAction(actionExecutors.pause, onBulkComplete)
  const bulkResume = useBulkAction(actionExecutors.resume, onBulkComplete)
  const bulkTrigger = useBulkAction(actionExecutors.trigger, onBulkComplete)

  const bulkActions: Record<
    PendingAction,
    ReturnType<typeof useBulkAction>
  > = useMemo(
    () => ({
      delete: bulkDelete,
      pause: bulkPause,
      resume: bulkResume,
      trigger: bulkTrigger,
    }),
    [bulkDelete, bulkPause, bulkResume, bulkTrigger]
  )

  const isRunning = Object.values(bulkActions).some((a) => a.running)

  const handleToggle = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleToggleAll = useCallback(() => {
    if (!data) return
    setSelectedIds((prev) => {
      if (prev.size === data.schedules.length) return new Set()
      return new Set(data.schedules.map((s) => s.id))
    })
  }, [data])

  const handleSelect = useCallback(
    (item: ListItem) => {
      const schedule = data?.schedules.find((s) => s.id === item.id)
      push({ view: 'schedule-detail', scheduleId: item.id, schedule })
    },
    [push, data]
  )

  const handleConfirm = useCallback(() => {
    if (!pendingAction || selectedIds.size === 0) return
    bulkActions[pendingAction].execute([...selectedIds])
    const count = selectedIds.size
    const label = actionLabels[pendingAction].toLowerCase()
    setToast({ message: `${label} ${count} schedule(s)...`, type: 'success' })
    setPendingAction(null)
  }, [pendingAction, selectedIds, bulkActions])

  const handleCancel = useCallback(() => {
    setPendingAction(null)
  }, [])

  const extraKeys = useCallback(
    (input: string) => {
      if (isRunning || pendingAction) return
      if (input === 'q') {
        exit()
        return
      }
      if (selectedIds.size === 0) return
      if (input === 'd') setPendingAction('delete')
      if (input === 'p') setPendingAction('pause')
      if (input === 'r') setPendingAction('resume')
      if (input === 't') setPendingAction('trigger')
    },
    [selectedIds, isRunning, pendingAction, exit]
  )

  const items: ListItem[] = useMemo(() => {
    if (!data) return []
    return data.schedules.map((s) => {
      const row = formatScheduleRow(s)
      return {
        id: s.id,
        label: row.name,
        columns: [row.destination, row.status, row.lastRun],
      }
    })
  }, [data])

  const footer = (
    <Box flexDirection="column" gap={0}>
      {selectedIds.size > 0 && (
        <Text color={RETRO.colors.highlight}>{selectedIds.size} selected</Text>
      )}
      <ActionBar
        actions={[
          { key: 'Space', label: 'Toggle' },
          { key: 'a', label: 'All' },
          { key: 'Enter', label: 'Detail' },
          { key: 'Esc', label: 'Menu' },
        ]}
      />
      {selectedIds.size > 0 && (
        <ActionBar
          actions={[
            { key: 'd', label: 'Delete' },
            { key: 'p', label: 'Pause' },
            { key: 'r', label: 'Resume' },
            { key: 't', label: 'Trigger' },
          ]}
        />
      )}
    </Box>
  )

  return (
    <Box flexDirection="column">
      <ResourceFrame
        title="SCHEDULES"
        loading={loading}
        error={error}
        count={data?.totalRecords}
        loadingLabel="Loading schedules..."
        footer={pendingAction ? undefined : footer}
        borderless
      >
        <SelectableList
          items={items}
          multiSelect
          selectedIds={selectedIds}
          onToggle={handleToggle}
          onToggleAll={handleToggleAll}
          onSelect={handleSelect}
          onBack={focusSidebar}
          extraKeys={extraKeys}
          active={isDetailActive && !pendingAction}
        />
      </ResourceFrame>

      {pendingAction && (
        <ConfirmDialog
          message={`${actionLabels[pendingAction]} ${selectedIds.size} schedule(s)?`}
          onConfirm={handleConfirm}
          onCancel={handleCancel}
        />
      )}

      {toast && (
        <ToastMessage
          message={toast.message}
          type={toast.type}
          onDismiss={() => setToast(null)}
        />
      )}
    </Box>
  )
}
