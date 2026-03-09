import type { ColumnDef, OutputFormat, TabularData } from './types.js'

export const renderJson = (data: unknown): void => {
  process.stdout.write(JSON.stringify(data, null, 2) + '\n')
}

export const renderCsv = (tabular: TabularData): void => {
  const escapeCsv = (value: unknown): string => {
    const str = value === null || value === undefined ? '' : String(value)
    if (str.includes(',') || str.includes('"') || str.includes('\n')) {
      return `"${str.replace(/"/g, '""')}"`
    }
    return str
  }

  const header = tabular.columns.map((col) => escapeCsv(col.header)).join(',')
  process.stdout.write(header + '\n')

  for (const row of tabular.rows) {
    const line = tabular.columns
      .map((col) => escapeCsv(row[col.key]))
      .join(',')
    process.stdout.write(line + '\n')
  }
}

export const renderPlainTable = (tabular: TabularData): void => {
  const widths = tabular.columns.map((col) => {
    const headerLen = col.header.length
    const maxDataLen = Math.max(
      ...tabular.rows.map(
        (row) => String(row[col.key] ?? '').length
      ),
      0
    )
    return col.width ?? Math.max(headerLen, maxDataLen) + 2
  })

  const padRight = (str: string, len: number): string =>
    str.padEnd(len).slice(0, len)

  const headerLine = tabular.columns
    .map((col, i) => padRight(col.header, widths[i]))
    .join('')
  process.stdout.write(headerLine + '\n')

  const separator = widths.map((w) => '-'.repeat(w)).join('')
  process.stdout.write(separator + '\n')

  for (const row of tabular.rows) {
    const line = tabular.columns
      .map((col, i) => padRight(String(row[col.key] ?? ''), widths[i]))
      .join('')
    process.stdout.write(line + '\n')
  }
}

export const renderPosix = (
  format: OutputFormat,
  data: unknown,
  tabular?: TabularData
): void => {
  switch (format) {
    case 'json':
      renderJson(data)
      break
    case 'csv':
      if (tabular) {
        renderCsv(tabular)
      } else {
        renderJson(data)
      }
      break
    case 'table':
      if (tabular) {
        renderPlainTable(tabular)
      } else {
        renderJson(data)
      }
      break
  }
}

export const renderPosixError = (message: string): void => {
  process.stderr.write(`Error: ${message}\n`)
}

export const renderPosixSuccess = (message: string): void => {
  process.stdout.write(`${message}\n`)
}
