export const outputFlags = {
  format: {
    type: String,
    alias: 'F',
    description: 'Output format: json, csv, or table',
  },
  'no-tui': {
    type: Boolean,
    description: 'Disable interactive TUI output',
  },
  'no-color': {
    type: Boolean,
    description: 'Disable colored output',
  },
} as const
