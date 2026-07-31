import { describe, it, expect } from 'vitest'
import { createMockPluginFs, createMockRemoteFS } from './mockFs'

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

  it('anchors any session cwd to the demo tree', async () => {
    const fs = createMockPluginFs()
    // 会话 cwd 可能是 ~/srv/atterm 等,与 demo 树根不同 — 仍应看到同一棵树
    const entries = await fs.listDir('~/srv/atterm')
    expect(entries.map((e) => e.name)).toContain('README.md')
    const sub = await fs.listDir('~/srv/atterm/src')
    expect(sub.map((e) => e.name)).toContain('app.ts')
  })

  it('assetUrlFor returns a blob url for binary files', () => {
    const fs = createMockPluginFs()
    const url = fs.assetUrlFor(`${ROOT}/logo.png`)
    expect(url.startsWith('blob:')).toBe(true)
  })
})

describe('createMockRemoteFS.handleFSRequest', () => {
  const call = (op: string, extra: Record<string, unknown> = {}) =>
    createMockRemoteFS().handleFSRequest({ op, request_id: 'r1', ...extra })

  it('list_dir returns entries anchored to any cwd', () => {
    const r = createMockRemoteFS().handleFSRequest({ op: 'list_dir', path: '~/projects/atterm', request_id: 'r1' })
    expect(r.ok).toBe(true)
    expect(r.entries?.map((e) => e.name)).toContain('main.go')
    expect(r.request_id).toBe('r1')
  })

  it('read_file returns base64 data', () => {
    const r = createMockRemoteFS().handleFSRequest({ op: 'read_file', path: '~/projects/atterm/README.md', request_id: 'r1' })
    expect(r.ok).toBe(true)
    const text = atob(r.content!.data)
    expect(text).toContain('atterm-demo')
  })

  it('write_file then read_file round-trips', () => {
    const fs = createMockRemoteFS()
    const w = fs.handleFSRequest({ op: 'write_file', path: '~/p/README.md', data: btoa('hello'), request_id: 'r1' })
    expect(w.ok).toBe(true)
    const r = fs.handleFSRequest({ op: 'read_file', path: '~/p/README.md', request_id: 'r2' })
    expect(atob(r.content!.data)).toBe('hello')
  })

  it('read_chunk returns eof for a small file', () => {
    const r = createMockRemoteFS().handleFSRequest({ op: 'read_chunk', path: '~/projects/atterm/logo.png', offset: 0, length: 262144, request_id: 'r1' })
    expect(r.ok).toBe(true)
    expect(r.chunk?.eof).toBe(true)
    expect(r.chunk?.contentType).toBe('image/png')
  })

  it('fails cleanly on unsupported op', () => {
    const r = call('frobnicate')
    expect(r.ok).toBe(false)
    expect(r.error).toContain('unsupported')
  })
})
