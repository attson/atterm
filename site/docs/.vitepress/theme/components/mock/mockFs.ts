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
  // 96x96 可见 PNG(深蓝底 + 蓝色圆 + 白色终端光标图案),用于文件浏览器图片
  // 预览演示。1x1 透明 PNG 预览出来看不见,故改用有内容的图。
  const b64 =
    'iVBORw0KGgoAAAANSUhEUgAAAGAAAABgCAYAAADimHc4AAAB3ElEQVR4nO3du3EDQRADUfpyFILyUkrKmcxAdfttYAZTBV/br+QeX1/fP++M24v+A7ovAAHovQAEoPcCEIDeC0AAei8AAfh/v3/v5dFvsALYEdwJRAbgRnhFCByACK8EgQHQ0VUgrgPQkdUgrgLQYRURrgDQMZUhjgPQAdURjgLQ4RwQjgHQwVwQjgDQoZwQtgPQgdwQtgLQYRwRtgHQQVwRAlABgA7hjLAMQAdQWAC6AtAPV1oAugHQD1ZcALoA0A9VXgCqA9APdFgAAtB7AagKcPMRT44OvQNBEuDp0ZFLAowcHbkcwOjRkUsBzBwduQzA7NGRSwCsHB3ZHmD16MjWADuOjmwNkP8AAYBVBDpyCYAVBDpyGYBZBDpyKYAZBDpyOYBRBDpySYARBDpyWYCnCHTk6wC3EVw30jMAAai31gA7ThpAHYEGGG0ZAEcAZQQSYKZjAFwBVBEogNmGAXAGUEW4vZV+AXAH6I6w2i7figDjB6ASQDeEXc3yxSww/hGA6gi7W+WriWD8owDVEE41ypdzwfhXANwRTrfJ19Oh8AiAC8LNHvkFDSg8DqAEQb4fByAh6DdLAdyEoN8oDXAChH6DNUD1BSAAvReAAPReAALQewEIQO8FAN4HvTH7ZldQiOQAAAAASUVORK5CYII='
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

  // 文件浏览器按活动会话的 cwd 浏览(如 ~/srv/atterm、~/projects/atterm),
  // 而假树只有一棵。把「当前浏览根」动态锚定:任何不在已知相对结构内的路径
  // 都被当作根 cwd,映射到 demo 树顶层。这样无论会话 cwd 是什么,打开文件面板
  // 都能看到同一棵 demo 树。
  let baseCwd = ROOT

  function resolve(path: string): { parent: Node | null; node: Node | null; name: string } {
    // path 若不以当前 baseCwd 开头,说明切到了新会话的 cwd — 重新锚定。
    if (!path.startsWith(baseCwd)) {
      // 若 path 是另一个 base 的子路径(含 '/atterm-demo-file' 之类),优先保留
      // 已锚定的 base;否则把 path 本身当作新根。
      baseCwd = path
    }
    const rel = path.slice(baseCwd.length).replace(/^\/+/, '')
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

// ---------------------------------------------------------------------------
// Remote 会话的文件浏览器走 createRemoteSessionFS(conn) → conn.sendFSRequest,
// 底层是 FS_REQUEST / FS_RESPONSE 帧(见 lib/connection.ts),而不是
// platform.pluginHost.fs。demo 里所有会话都是 remote,所以文件面板实际走这条
// 路。下面提供一个基于同一棵内存树的 FS_REQUEST 处理器,由 mockSocket 调用。
//
// FSResponse 字段(Go 侧命名):entries: FSDirEntry[]{name,isDir,size,modTime}、
// content: {path,data(base64),isBinary,truncatedAt}、meta:
// {path,size,modTime,isBinary}、chunk: {path,data(base64),offset,length,eof,
// contentType}、watch_id。data 一律 base64 字符串(Go []byte JSON 编码)。

export interface FSRequestLike {
  op: string
  path?: string
  new_path?: string
  data?: string // base64
  offset?: number
  length?: number
  recursive?: boolean
  request_id?: string
  [k: string]: unknown
}

export interface FSResponseLike {
  request_id: string
  ok: boolean
  error?: string
  entries?: Array<{ name: string; isDir: boolean; size: number; modTime: number }>
  meta?: { path: string; size: number; modTime: number; isBinary: boolean }
  content?: { path: string; data: string; isBinary: boolean; truncatedAt?: number }
  chunk?: { path: string; data: string; offset: number; length: number; eof: boolean; contentType?: string }
  watch_id?: string
}

function contentTypeFor(name: string): string {
  if (name.endsWith('.png')) return 'image/png'
  if (name.endsWith('.pdf')) return 'application/pdf'
  return 'application/octet-stream'
}

export function createMockRemoteFS() {
  const root = seed()
  let baseCwd = ROOT

  // 只有 list_dir 会用一个「目录根」路径来重新锚定 base(那时 path 一定是目录)。
  // read_file/write_file 等操作的 path 是 base 下的子路径,不应改动 base。
  function setBase(path: string) {
    baseCwd = path
  }

  function resolve(path: string): { parent: Node | null; node: Node | null; name: string } {
    // path 以 baseCwd 开头:取相对部分。否则可能是根目录本身(list_dir 已 setBase)
    // 或异常路径 — 退化为把 path 末段当作 base 下的名字处理。
    const rel = path.startsWith(baseCwd) ? path.slice(baseCwd.length).replace(/^\/+/, '') : path.replace(/^.*\//, '')
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

  const isBinary = (n: Node): boolean => n.name.endsWith('.png') || n.name.endsWith('.pdf')
  const metaOf = (path: string, n: Node) => ({ path, size: n.data?.byteLength ?? 0, modTime: n.modTime, isBinary: isBinary(n) })

  function handleFSRequest(req: FSRequestLike): FSResponseLike {
    const id = String(req.request_id ?? '')
    const ok = (extra: Partial<FSResponseLike>): FSResponseLike => ({ request_id: id, ok: true, ...extra })
    const fail = (error: string): FSResponseLike => ({ request_id: id, ok: false, error })
    try {
      switch (req.op) {
        case 'list_dir': {
          // 若列的目录不在当前 base 下,说明切到了新会话 cwd — 以它为新根。
          if (req.path && !req.path.startsWith(baseCwd)) setBase(req.path)
          const { node } = resolve(req.path!)
          if (!node?.dir) return fail('not a directory')
          const entries = Array.from(node.children!.values())
            .sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))
            .map((n) => ({ name: n.name, isDir: n.dir, size: n.data?.byteLength ?? 0, modTime: n.modTime }))
          return ok({ entries })
        }
        case 'read_file': {
          const { node } = resolve(req.path!)
          if (!node || node.dir) return fail('not a file')
          const bytes = node.data ?? new Uint8Array()
          return ok({
            content: { path: req.path!, data: bytesToB64(bytes), isBinary: isBinary(node) },
          })
        }
        case 'file_meta': {
          const { node } = resolve(req.path!)
          if (!node) return fail('not found')
          return ok({ meta: metaOf(req.path!, node) })
        }
        case 'watch_dir':
          return ok({ watch_id: 'w-' + Math.abs(hashPath(req.path ?? '')) })
        case 'unwatch_dir':
          return ok({})
        case 'open_external':
          return ok({})
        case 'write_file': {
          const { parent, name, node } = resolve(req.path!)
          const bytes = req.data ? b64ToBytes(req.data) : new Uint8Array()
          if (node) {
            node.data = bytes
            node.modTime++
            return ok({ meta: metaOf(req.path!, node) })
          }
          if (!parent) return fail('no parent')
          const n: Node = { name, dir: false, data: bytes, modTime: 1 }
          parent.children!.set(name, n)
          return ok({ meta: metaOf(req.path!, n) })
        }
        case 'create_file': {
          const { parent, name } = resolve(req.path!)
          if (!parent) return fail('no parent')
          const n: Node = { name, dir: false, data: new Uint8Array(), modTime: 1 }
          parent.children!.set(name, n)
          return ok({ meta: metaOf(req.path!, n) })
        }
        case 'mkdir': {
          const { parent, name } = resolve(req.path!)
          if (!parent) return fail('no parent')
          const n: Node = { name, dir: true, modTime: 1, children: new Map() }
          parent.children!.set(name, n)
          return ok({ meta: metaOf(req.path!, n) })
        }
        case 'rename': {
          const src = resolve(req.path!)
          const dst = resolve(req.new_path!)
          if (!src.node || !src.parent || !dst.parent) return fail('bad rename')
          src.parent.children!.delete(src.name)
          src.node.name = dst.name
          dst.parent.children!.set(dst.name, src.node)
          return ok({ meta: metaOf(req.new_path!, src.node) })
        }
        case 'remove':
        case 'trash': {
          const { parent, name } = resolve(req.path!)
          parent?.children!.delete(name)
          return ok({})
        }
        case 'read_chunk': {
          const { node } = resolve(req.path!)
          if (!node || node.dir) return fail('not a file')
          const bytes = node.data ?? new Uint8Array()
          const offset = req.offset ?? 0
          const length = req.length ?? bytes.byteLength
          const slice = bytes.slice(offset, offset + length)
          const eof = offset + slice.byteLength >= bytes.byteLength
          return ok({
            chunk: {
              path: req.path!,
              data: bytesToB64(slice),
              offset,
              length: slice.byteLength,
              eof,
              contentType: contentTypeFor(node.name),
            },
          })
        }
        default:
          return fail(`unsupported op: ${req.op}`)
      }
    } catch (e) {
      return fail(e instanceof Error ? e.message : String(e))
    }
  }

  return { handleFSRequest }
}

function hashPath(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return h
}
