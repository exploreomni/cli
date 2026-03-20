import { useCallback, useState } from 'react'

interface BulkActionState {
  running: boolean
  results: { id: string; success: boolean; error?: string }[] | null
}

export const useBulkAction = (
  actionFn: (id: string) => Promise<unknown>,
  onComplete?: () => void
): BulkActionState & { execute: (ids: string[]) => void } => {
  const [state, setState] = useState<BulkActionState>({
    running: false,
    results: null,
  })

  const execute = useCallback(
    (ids: string[]) => {
      setState({ running: true, results: null })

      Promise.allSettled(ids.map((id) => actionFn(id))).then((outcomes) => {
        const results = outcomes.map((outcome, i) => ({
          id: ids[i],
          success: outcome.status === 'fulfilled',
          error:
            outcome.status === 'rejected'
              ? outcome.reason instanceof Error
                ? outcome.reason.message
                : String(outcome.reason)
              : undefined,
        }))
        setState({ running: false, results })
        onComplete?.()
      })
    },
    [actionFn, onComplete]
  )

  return { ...state, execute }
}
