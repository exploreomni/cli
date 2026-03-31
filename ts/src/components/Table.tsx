import { Box, Text } from 'ink'
import type React from 'react'

interface Column {
  key: string
  header: string
  width?: number
}

interface TableProps {
  columns: Column[]
  data: Record<string, unknown>[]
}

export const Table: React.FC<TableProps> = ({ columns, data }) => {
  const getColumnWidth = (col: Column): number => {
    if (col.width) return col.width
    const headerLen = col.header.length
    const maxDataLen = Math.max(
      ...data.map((row) => String(row[col.key] ?? '').length),
      0
    )
    return Math.max(headerLen, maxDataLen) + 2
  }

  const widths = columns.map(getColumnWidth)

  const padRight = (str: string, len: number): string => {
    return str.padEnd(len).slice(0, len)
  }

  return (
    <Box flexDirection="column">
      <Box>
        {columns.map((col, i) => (
          <Text key={col.key} bold color="cyan">
            {padRight(col.header, widths[i])}
          </Text>
        ))}
      </Box>
      <Box>
        {columns.map((col, i) => (
          <Text key={col.key} dimColor>
            {'-'.repeat(widths[i])}
          </Text>
        ))}
      </Box>
      {data.map((row, rowIndex) => (
        <Box key={rowIndex}>
          {columns.map((col, i) => (
            <Text key={col.key}>
              {padRight(String(row[col.key] ?? ''), widths[i])}
            </Text>
          ))}
        </Box>
      ))}
    </Box>
  )
}
