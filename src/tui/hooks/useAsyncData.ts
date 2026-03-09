import { useState, useEffect, useCallback } from 'react'

interface AsyncDataState<T> {
  data: T | null
  loading: boolean
  error: string | null
  reload: () => void
}

export const useAsyncData = <T>(
  fetcher: () => Promise<T>
): AsyncDataState<T> => {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)

  const reload = useCallback(() => {
    setReloadKey((k) => k + 1)
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)

    fetcher()
      .then((result) => {
        if (!cancelled) {
          setData(result)
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setLoading(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [reloadKey]) // eslint-disable-line react-hooks/exhaustive-deps

  return { data, loading, error, reload }
}
