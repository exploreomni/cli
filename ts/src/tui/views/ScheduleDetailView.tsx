import { Box, Text, useApp, useInput } from 'ink'
import { useCallback, useState } from 'react'
import type { ScheduleListItem } from '../../api/index.js'
import { executeScheduleDelete } from '../../commands/schedule/delete.execute.js'
import { formatScheduleRow } from '../../commands/schedule/list.execute.js'
import { executeSchedulePause } from '../../commands/schedule/pause.execute.js'
import { executeScheduleResume } from '../../commands/schedule/resume.execute.js'
import { executeScheduleTrigger } from '../../commands/schedule/trigger.execute.js'
import {
  ActionBar,
  ConfirmDialog,
  KeyValueFields,
  RetroFrame,
  ToastMessage,
} from '../components/index.js'
import { usePaneFocus } from '../focus.js'
import { useRouter } from '../router.js'
import { RETRO } from '../theme.js'

type ActionType = 'delete' | 'pause' | 'resume' | 'trigger'

const actionLabels: Record<ActionType, string> = {
  delete: 'Delete',
  pause: 'Pause',
  resume: 'Resume',
  trigger: 'Trigger',
}

export const ScheduleDetailView = ({
  scheduleId,
  schedule,
}: {
  scheduleId: string
  schedule?: ScheduleListItem
}) => {
  const { pop } = useRouter()
  const { exit } = useApp()
  const { isDetailActive } = usePaneFocus()
  const [pendingAction, setPendingAction] = useState<ActionType | null>(null)
  const [toast, setToast] = useState<{
    message: string
    type: 'success' | 'error'
  } | null>(null)

  const runAction = useCallback(
    async (action: ActionType) => {
      try {
        if (action === 'delete') await executeScheduleDelete({ scheduleId })
        else if (action === 'pause') await executeSchedulePause({ scheduleId })
        else if (action === 'resume')
          await executeScheduleResume({ scheduleId })
        else if (action === 'trigger')
          await executeScheduleTrigger({ scheduleId })

        setToast({
          message: `${actionLabels[action]} successful`,
          type: 'success',
        })
        if (action === 'delete') {
          pop()
        }
      } catch (err: unknown) {
        setToast({
          message: `Failed: ${err instanceof Error ? err.message : String(err)}`,
          type: 'error',
        })
      }
    },
    [scheduleId, pop]
  )

  useInput(
    (input, key) => {
      if (pendingAction) return
      if (key.escape) {
        pop()
        return
      }
      if (input === 'q') {
        exit()
        return
      }
      if (input === 'd') setPendingAction('delete')
      if (input === 'p') setPendingAction('pause')
      if (input === 'r') setPendingAction('resume')
      if (input === 't') setPendingAction('trigger')
    },
    { isActive: isDetailActive && !pendingAction }
  )

  if (!schedule) {
    return (
      <RetroFrame title="SCHEDULE DETAIL" borderless>
        <Text color={RETRO.colors.error}>Schedule not found</Text>
      </RetroFrame>
    )
  }

  const row = formatScheduleRow(schedule)
  const s = schedule

  const fields: [string, string][] = [
    ['Name', row.name],
    ['Dashboard', row.dashboard],
    ['Owner', row.owner],
    ['Destination', row.destination],
    ['Format', row.format],
    ['Status', row.status],
    ['Schedule', s.schedule],
    ['Timezone', s.timezone],
    ['Last Run', row.lastRun],
    ['Last Status', row.lastStatus],
    ['Recipients', String(s.recipientCount)],
    ['ID', s.id],
  ]

  return (
    <Box flexDirection="column">
      <RetroFrame
        title={`SCHEDULE: ${row.name}`}
        borderless
        footer={
          pendingAction ? undefined : (
            <ActionBar
              actions={[
                { key: 'Esc', label: 'Back' },
                { key: 'd', label: 'Delete' },
                { key: 'p', label: 'Pause' },
                { key: 'r', label: 'Resume' },
                { key: 't', label: 'Trigger' },
                { key: 'q', label: 'Quit' },
              ]}
            />
          )
        }
      >
        <KeyValueFields fields={fields} />
      </RetroFrame>

      {pendingAction && (
        <ConfirmDialog
          message={`${actionLabels[pendingAction]} this schedule?`}
          onConfirm={() => {
            const action = pendingAction
            setPendingAction(null)
            runAction(action)
          }}
          onCancel={() => setPendingAction(null)}
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
