import type { OutputFormat, OutputMode } from './types.js'

export const resolveOutputMode = (flags: {
  format?: string
  noTui?: boolean
  noColor?: boolean
}): OutputMode => {
  const hasFormatFlag = flags.format === 'json' || flags.format === 'csv'
  const noTui = flags.noTui === true
  const isTTY = process.stdout.isTTY === true
  const noColor = flags.noColor === true || process.env.NO_COLOR !== undefined

  const isTUI = !hasFormatFlag && !noTui && isTTY

  let format: OutputFormat
  if (flags.format === 'json' || flags.format === 'csv') {
    format = flags.format
  } else if (isTUI) {
    format = 'table'
  } else {
    format = 'json'
  }

  return {
    isTUI,
    format,
    color: !noColor && isTTY,
  }
}
