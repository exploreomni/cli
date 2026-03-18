import { renderJson, renderCsv, renderPlainTable, renderPosix, renderPosixError, renderPosixSuccess } from './posix.js'

describe('posix output', () => {
  let stdoutSpy: ReturnType<typeof vi.spyOn>
  let stderrSpy: ReturnType<typeof vi.spyOn>

  const collectStdout = (): string =>
    stdoutSpy.mock.calls.map((call: [string]) => call[0]).join('')

  const collectStderr = (): string =>
    stderrSpy.mock.calls.map((call: [string]) => call[0]).join('')

  beforeEach(() => {
    stdoutSpy = vi.spyOn(process.stdout, 'write').mockReturnValue(true)
    stderrSpy = vi.spyOn(process.stderr, 'write').mockReturnValue(true)
  })

  afterEach(() => {
    stdoutSpy.mockRestore()
    stderrSpy.mockRestore()
  })

  describe('renderJson', () => {
    it('outputs valid pretty-printed JSON', () => {
      const data = { name: 'test', count: 42 }
      renderJson(data)
      const output = collectStdout()
      expect(JSON.parse(output)).toEqual(data)
      expect(output).toContain('\n')
      expect(output).toContain('  ')
    })
  })

  describe('renderCsv', () => {
    const tabular = {
      columns: [
        { key: 'name', header: 'Name' },
        { key: 'value', header: 'Value' },
      ],
      rows: [
        { name: 'alpha', value: '1' },
        { name: 'beta', value: '2' },
      ],
    }

    it('outputs header + data rows', () => {
      renderCsv(tabular)
      const output = collectStdout()
      const lines = output.trim().split('\n')
      expect(lines[0]).toBe('Name,Value')
      expect(lines[1]).toBe('alpha,1')
      expect(lines[2]).toBe('beta,2')
    })

    it('escapes values with commas, quotes, newlines', () => {
      const tabularSpecial = {
        columns: [{ key: 'val', header: 'Val' }],
        rows: [
          { val: 'has,comma' },
          { val: 'has"quote' },
          { val: 'has\nnewline' },
        ],
      }
      renderCsv(tabularSpecial)
      const output = collectStdout()
      const lines = output.trim().split('\n')
      expect(lines[1]).toBe('"has,comma"')
      expect(lines[2]).toBe('"has""quote"')
    })
  })

  describe('renderPlainTable', () => {
    it('outputs header, separator, and aligned rows', () => {
      const tabular = {
        columns: [
          { key: 'name', header: 'Name' },
          { key: 'id', header: 'ID' },
        ],
        rows: [
          { name: 'alpha', id: '1' },
          { name: 'beta', id: '2' },
        ],
      }
      renderPlainTable(tabular)
      const output = collectStdout()
      const lines = output.trim().split('\n')
      expect(lines[0]).toContain('Name')
      expect(lines[0]).toContain('ID')
      expect(lines[1]).toMatch(/^-+$/)
      expect(lines[2]).toContain('alpha')
      expect(lines[3]).toContain('beta')
    })
  })

  describe('renderPosix', () => {
    const tabular = {
      columns: [{ key: 'name', header: 'Name' }],
      rows: [{ name: 'test' }],
    }

    it('routes to renderJson for json format', () => {
      const data = { items: [1, 2] }
      renderPosix('json', data)
      const output = collectStdout()
      expect(JSON.parse(output)).toEqual(data)
    })

    it('routes to renderCsv for csv with tabular data', () => {
      renderPosix('csv', {}, tabular)
      const output = collectStdout()
      expect(output).toContain('Name')
      expect(output).toContain('test')
    })

    it('falls back to JSON for csv without tabular data', () => {
      const data = { x: 1 }
      renderPosix('csv', data)
      const output = collectStdout()
      expect(JSON.parse(output)).toEqual(data)
    })

    it('falls back to JSON for table without tabular data', () => {
      const data = { x: 1 }
      renderPosix('table', data)
      const output = collectStdout()
      expect(JSON.parse(output)).toEqual(data)
    })
  })

  describe('renderPosixError', () => {
    it('writes to stderr', () => {
      renderPosixError('something failed')
      expect(collectStderr()).toBe('Error: something failed\n')
    })
  })

  describe('renderPosixSuccess', () => {
    it('writes to stdout', () => {
      renderPosixSuccess('done')
      expect(collectStdout()).toBe('done\n')
    })
  })
})
