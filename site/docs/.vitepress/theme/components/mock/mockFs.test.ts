import { describe, it, expect } from 'vitest'
import { createMockPluginFs } from './mockFs'

const ROOT = '~/projects/atterm-demo'

describe('createMockPluginFs', () => {
  it('lists the demo project root', async () => {
    const fs = createMockPluginFs()
    const entries = await fs.listDir(ROOT)
    const names = entries.map((e) => e.name)
    expect(names).toContain('README.md')
    expect(names).toContain('main.go')
    expect(names).toContain('src')
  })

  it('sorts directories before files', async () => {
    const fs = createMockPluginFs()
    const entries = await fs.listDir(ROOT)
    expect(entries[0].isDir).toBe(true) // src first
    expect(entries[0].name).toBe('src')
  })

  it('reads a text file as byte array', async () => {
    const fs = createMockPluginFs()
    const f = await fs.readFile(`${ROOT}/README.md`)
    const text = new TextDecoder().decode(new Uint8Array(f.data))
    expect(text).toContain('atterm-demo')
    expect(f.isBinary).toBe(false)
  })

  it('writes then reads back', async () => {
    const fs = createMockPluginFs()
    const bytes = Array.from(new TextEncoder().encode('changed'))
    await fs.writeFile(`${ROOT}/README.md`, bytes, 0, false)
    const f = await fs.readFile(`${ROOT}/README.md`)
    expect(new TextDecoder().decode(new Uint8Array(f.data))).toContain('changed')
  })

  it('mkdir + createFile + rename + remove', async () => {
    const fs = createMockPluginFs()
    await fs.mkdir(`${ROOT}/newdir`)
    await fs.createFile(`${ROOT}/newdir/a.txt`)
    await fs.rename(`${ROOT}/newdir/a.txt`, `${ROOT}/newdir/b.txt`)
    const entries = await fs.listDir(`${ROOT}/newdir`)
    expect(entries.map((e) => e.name)).toEqual(['b.txt'])
    await fs.remove(`${ROOT}/newdir/b.txt`, false)
    const after = await fs.listDir(`${ROOT}/newdir`)
    expect(after.length).toBe(0)
  })

  it('trash removes an entry', async () => {
    const fs = createMockPluginFs()
    await fs.trash(`${ROOT}/package.json`)
    const entries = await fs.listDir(ROOT)
    expect(entries.map((e) => e.name)).not.toContain('package.json')
  })

  it('assetUrlFor returns a blob url for binary files', () => {
    const fs = createMockPluginFs()
    const url = fs.assetUrlFor(`${ROOT}/logo.png`)
    expect(url.startsWith('blob:')).toBe(true)
  })
})
