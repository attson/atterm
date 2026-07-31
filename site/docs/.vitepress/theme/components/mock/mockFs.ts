import type { PluginModels } from '@/platform/types'

// mock 的是 platform.pluginHost.fs(见 desktop/frontend/src/platform/types.ts
// 的 PluginHostBridge.fs)。文件浏览器在 caps.pluginHost=true 时通过
// createLocalFSBridge(platform.pluginHost) 包装这套方法,所以这里实现它即可,
// 无需自己实现 FileSystemBridge。字段用 wailsjs models 的 camelCase 命名:
// DirEntry{name,isDir,size,modTime}、FileContent{path,data:number[],isBinary,
// truncatedAt}、FileMetaInfo{path,size,modTime,isBinary}。

type DirEntry = PluginModels.DirEntry
type FileContent = PluginModels.FileContent
type FileMetaInfo = PluginModels.FileMetaInfo

// PluginHostBridge.fs 的结构(从 platform/types.ts 抄成本地形状,避免依赖
// 未导出的内部类型)。
export interface MockPluginFs {
  listDir(path: string): Promise<DirEntry[]>
  watchDir(path: string): Promise<number>
  unwatchDir(id: number): Promise<void>
  readFile(path: string, maxBytes?: number): Promise<FileContent>
  fileMeta(path: string): Promise<FileMetaInfo>
  openExternal(path: string): Promise<void>
  assetUrlFor(path: string): string
  writeFile(
    path: string,
    data: number[] | Uint8Array,
    expectedModTime: number,
    createIfMissing: boolean,
  ): Promise<FileMetaInfo>
  createFile(path: string): Promise<FileMetaInfo>
  rename(from: string, to: string): Promise<FileMetaInfo>
  remove(path: string, recursive: boolean): Promise<void>
  mkdir(path: string): Promise<FileMetaInfo>
  trash(path: string): Promise<void>
}

interface Node {
  name: string
  dir: boolean
  data?: Uint8Array
  children?: Map<string, Node>
  modTime: number
}

const ROOT = '~/projects/atterm-demo'

function seed(): Node {
  const enc = new TextEncoder()
  const file = (name: string, text: string): Node => ({ name, dir: false, data: enc.encode(text), modTime: 1 })
  const root: Node = { name: 'atterm-demo', dir: true, modTime: 1, children: new Map() }
  const src: Node = { name: 'src', dir: true, modTime: 1, children: new Map() }
  src.children!.set('app.ts', file('app.ts', 'export const app = () => "hi"\n'))
  src.children!.set('util.ts', file('util.ts', 'export const add = (a: number, b: number) => a + b\n'))
  root.children!.set('README.md', file('README.md', '# atterm-demo\n\nA fake project for the demo.\n'))
  root.children!.set('main.go', file('main.go', 'package main\n\nfunc main() { println("hi") }\n'))
  root.children!.set('package.json', file('package.json', '{\n  "name": "atterm-demo"\n}\n'))
  root.children!.set('logo.png', pngNode())
  root.children!.set('src', src)
  return root
}

function pngNode(): Node {
  // 1x1 透明 PNG
  const b64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=='
  return { name: 'logo.png', dir: false, data: b64ToBytes(b64), modTime: 1 }
}

function b64ToBytes(b64: string): Uint8Array {
  const bin =
    typeof atob === 'function'
      ? atob(b64)
      : Buffer.from(b64, 'base64').toString('binary')
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return bytes
}

export function createMockPluginFs(): MockPluginFs {
  const root = seed()

  function resolve(path: string): { parent: Node | null; node: Node | null; name: string } {
    const rel = path.replace(ROOT, '').replace(/^\/+/, '')
    if (rel === '') return { parent: null, node: root, name: 'atterm-demo' }
    const parts = rel.split('/')
    let cur: Node = root
    for (let i = 0; i < parts.length - 1; i++) {
      const next = cur.children?.get(parts[i])
      if (!next || !next.dir) return { parent: null, node: null, name: parts[parts.length - 1] }
      cur = next
    }
    const name = parts[parts.length - 1]
    return { parent: cur, node: cur.children?.get(name) ?? null, name }
  }

  const toEntry = (n: Node): DirEntry =>
    ({ name: n.name, isDir: n.dir, size: n.data?.byteLength ?? 0, modTime: n.modTime } as DirEntry)

  const meta = (path: string, n: Node): FileMetaInfo =>
    ({ path, size: n.data?.byteLength ?? 0, modTime: n.modTime, isBinary: isBinary(n) } as FileMetaInfo)

  const isBinary = (n: Node): boolean => n.name.endsWith('.png') || n.name.endsWith('.pdf')

  return {
    async listDir(path) {
      const { node } = resolve(path)
      if (!node?.dir) throw new Error('not a directory')
      return Array.from(node.children!.values())
        .sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))
        .map(toEntry)
    },
    async watchDir() {
      return 0
    },
    async unwatchDir() {},
    async readFile(path) {
      const { node } = resolve(path)
      if (!node || node.dir) throw new Error('not a file')
      const bytes = node.data ?? new Uint8Array()
      return {
        path,
        data: Array.from(bytes),
        isBinary: isBinary(node),
        truncatedAt: undefined,
      } as FileContent
    },
    async fileMeta(path) {
      const { node } = resolve(path)
      if (!node) throw new Error('not found')
      return meta(path, node)
    },
    async openExternal() {},
    assetUrlFor(path) {
      const { node } = resolve(path)
      if (!node?.data) return ''
      if (typeof URL !== 'undefined' && typeof URL.createObjectURL === 'function') {
        return URL.createObjectURL(new Blob([node.data]))
      }
      // node/test 环境兜底
      return `data:application/octet-stream;base64,${bytesToB64(node.data)}`
    },
    async writeFile(path, data, _expectedModTime, createIfMissing) {
      const { parent, name, node } = resolve(path)
      const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
      if (node) {
        node.data = bytes
        node.modTime++
        return meta(path, node)
      }
      if (!parent || !createIfMissing) throw new Error('no such file')
      const n: Node = { name, dir: false, data: bytes, modTime: 1 }
      parent.children!.set(name, n)
      return meta(path, n)
    },
    async createFile(path) {
      const { parent, name } = resolve(path)
      if (!parent) throw new Error('no parent')
      const n: Node = { name, dir: false, data: new Uint8Array(), modTime: 1 }
      parent.children!.set(name, n)
      return meta(path, n)
    },
    async rename(from, to) {
      const src = resolve(from)
      const dst = resolve(to)
      if (!src.node || !src.parent || !dst.parent) throw new Error('bad rename')
      src.parent.children!.delete(src.name)
      src.node.name = dst.name
      dst.parent.children!.set(dst.name, src.node)
      return meta(to, src.node)
    },
    async remove(path) {
      const { parent, name } = resolve(path)
      parent?.children!.delete(name)
    },
    async mkdir(path) {
      const { parent, name } = resolve(path)
      if (!parent) throw new Error('no parent')
      const n: Node = { name, dir: true, modTime: 1, children: new Map() }
      parent.children!.set(name, n)
      return meta(path, n)
    },
    async trash(path) {
      const { parent, name } = resolve(path)
      parent?.children!.delete(name)
    },
  }
}

function bytesToB64(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return typeof btoa === 'function' ? btoa(bin) : Buffer.from(bin, 'binary').toString('base64')
}
