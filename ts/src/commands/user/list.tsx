import { Box, render, Text } from 'ink'
import type React from 'react'
import { useEffect, useState } from 'react'
import { Spinner, StatusMessage, Table } from '../../components/index.js'
import type { OutputMode, TabularData } from '../../output/index.js'
import {
  renderPosix,
  renderPosixError,
  resolveOutputMode,
} from '../../output/index.js'
import type { UserListResult } from './list.execute.js'
import { executeUserList, formatUserRow } from './list.execute.js'

interface UserListProps {
  search?: string
  count?: number
  profile?: string
}

const USER_COLUMNS = [
  { key: 'name', header: 'Name', width: 25 },
  { key: 'email', header: 'Email', width: 30 },
  { key: 'groups', header: 'Groups', width: 20 },
  { key: 'active', header: 'Active', width: 8 },
  { key: 'lastLogin', header: 'Last Login', width: 14 },
  { key: 'id', header: 'ID' },
]

const UserList: React.FC<UserListProps> = (props) => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<UserListResult | null>(null)

  useEffect(() => {
    executeUserList(props)
      .then((r) => setResult(r.data))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [props])

  if (loading) {
    return <Spinner label="Fetching users..." />
  }

  if (error) {
    return <StatusMessage status="error">{error}</StatusMessage>
  }

  if (!result || result.users.length === 0) {
    return (
      <Box flexDirection="column">
        <Text color="yellow">No users found.</Text>
      </Box>
    )
  }

  const tableData = result.users.map(formatUserRow)

  return (
    <Box flexDirection="column" gap={1}>
      <Text bold>Users ({result.totalResults})</Text>
      <Table columns={USER_COLUMNS} data={tableData} />
    </Box>
  )
}

export const runUserList = (options: {
  search?: string
  count?: number
  profile?: string
  outputMode?: OutputMode
}): void => {
  const mode = options.outputMode ?? resolveOutputMode({})

  if (mode.isTUI) {
    render(<UserList {...options} />)
    return
  }

  executeUserList(options)
    .then((result) => {
      const tabular: TabularData = {
        columns: USER_COLUMNS,
        rows: result.data.users.map(formatUserRow),
      }
      renderPosix(mode.format, result.data.users, tabular)
    })
    .catch((e: Error) => {
      renderPosixError(e.message)
      process.exit(1)
    })
}
