export type OutputFormat = 'json' | 'csv' | 'table'

export interface OutputMode {
  isTUI: boolean
  format: OutputFormat
  color: boolean
}

export interface ColumnDef {
  key: string
  header: string
  width?: number
}

export interface TabularData {
  columns: ColumnDef[]
  rows: Record<string, unknown>[]
}

export interface CommandResult<T> {
  data: T
  exitCode?: number
}
