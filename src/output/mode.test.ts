import { resolveOutputMode } from './mode.js'

describe('resolveOutputMode', () => {
  let originalIsTTY: boolean | undefined
  let originalNoColor: string | undefined

  beforeEach(() => {
    originalIsTTY = process.stdout.isTTY
    originalNoColor = process.env.NO_COLOR
  })

  afterEach(() => {
    process.stdout.isTTY = originalIsTTY as any
    if (originalNoColor === undefined) {
      delete process.env.NO_COLOR
    } else {
      process.env.NO_COLOR = originalNoColor
    }
  })

  it('TUI mode when isTTY=true, no format flags', () => {
    process.stdout.isTTY = true
    delete process.env.NO_COLOR
    expect(resolveOutputMode({})).toEqual({
      isTUI: true,
      format: 'table',
      color: true,
    })
  })

  it('JSON when format=json', () => {
    process.stdout.isTTY = true
    delete process.env.NO_COLOR
    const result = resolveOutputMode({ format: 'json' })
    expect(result.isTUI).toBe(false)
    expect(result.format).toBe('json')
  })

  it('CSV when format=csv', () => {
    process.stdout.isTTY = true
    delete process.env.NO_COLOR
    const result = resolveOutputMode({ format: 'csv' })
    expect(result.isTUI).toBe(false)
    expect(result.format).toBe('csv')
  })

  it('non-TTY defaults to JSON', () => {
    process.stdout.isTTY = undefined as any
    delete process.env.NO_COLOR
    const result = resolveOutputMode({})
    expect(result.isTUI).toBe(false)
    expect(result.format).toBe('json')
  })

  it('noTui=true disables TUI even if TTY', () => {
    process.stdout.isTTY = true
    delete process.env.NO_COLOR
    const result = resolveOutputMode({ noTui: true })
    expect(result.isTUI).toBe(false)
    expect(result.format).toBe('json')
  })

  it('noColor=true sets color to false', () => {
    process.stdout.isTTY = true
    delete process.env.NO_COLOR
    const result = resolveOutputMode({ noColor: true })
    expect(result.color).toBe(false)
  })

  it('NO_COLOR env var set sets color to false', () => {
    process.stdout.isTTY = true
    process.env.NO_COLOR = '1'
    const result = resolveOutputMode({})
    expect(result.color).toBe(false)
  })

  it('color is false when not TTY regardless of noColor', () => {
    process.stdout.isTTY = undefined as any
    delete process.env.NO_COLOR
    const result = resolveOutputMode({})
    expect(result.color).toBe(false)
  })
})
