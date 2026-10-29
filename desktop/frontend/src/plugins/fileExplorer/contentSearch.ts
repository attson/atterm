import type { FileSystemBridge } from "./fsBridge";

export interface FileContentSearchResult {
  path: string;
  name: string;
  line: number;
  preview: string;
}

export interface FileContentSearchOptions {
  showHidden: boolean;
  maxResults?: number;
  maxDirs?: number;
  maxFileBytes?: number;
  caseSensitive?: boolean;
  useRegex?: boolean;
  isCancelled?: () => boolean;
}

export interface FileContentSearchResponse {
  results: FileContentSearchResult[];
  truncated: boolean;
}

interface DirEntry { name: string; isDir: boolean; }

const DEFAULT_MAX_RESULTS = 200;
const DEFAULT_MAX_DIRS = 2_000;
const DEFAULT_MAX_FILE_BYTES = 512 * 1024;
const IGNORED_DIRS = new Set([".git", "node_modules", "dist", "build", ".next", ".turbo", "vendor"]);
const PSEUDO_ROOT_DIRS = new Set(["proc", "sys", "dev", "run"]);

function joinPath(parent: string, name: string): string {
  return parent.endsWith("/") ? parent + name : `${parent}/${name}`;
}

function decode(data: unknown): string | null {
  if (typeof data === "string") {
    try {
      const raw = atob(data);
      const bytes = new Uint8Array(raw.length);
      for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
      return new TextDecoder().decode(bytes);
    } catch { return null; }
  }
  if (data instanceof Uint8Array) return new TextDecoder().decode(data);
  if (Array.isArray(data)) return new TextDecoder().decode(new Uint8Array(data as number[]));
  return null;
}

/** Search text files below a session root without crawling generated trees. */
export async function searchFileContents(
  fs: FileSystemBridge,
  root: string,
  query: string,
  options: FileContentSearchOptions,
): Promise<FileContentSearchResponse> {
  const rawQuery = query.trim();
  if (!rawQuery) return { results: [], truncated: false };
  const q = options.caseSensitive ? rawQuery : rawQuery.toLocaleLowerCase();
  let matcher: (line: string) => boolean;
  if (options.useRegex) {
    try {
      const regex = new RegExp(rawQuery, options.caseSensitive ? "" : "i");
      matcher = (line) => regex.test(line);
    } catch {
      matcher = (line) => (options.caseSensitive ? line : line.toLocaleLowerCase()).includes(q);
    }
  } else {
    matcher = (line) => (options.caseSensitive ? line : line.toLocaleLowerCase()).includes(q);
  }
  const maxResults = options.maxResults ?? DEFAULT_MAX_RESULTS;
  const maxDirs = options.maxDirs ?? DEFAULT_MAX_DIRS;
  const maxFileBytes = options.maxFileBytes ?? DEFAULT_MAX_FILE_BYTES;
  const cancelled = options.isCancelled ?? (() => false);
  const queue = [root];
  const results: FileContentSearchResult[] = [];
  let visitedDirs = 0;
  let truncated = false;

  while (queue.length > 0) {
    if (cancelled()) return { results: [], truncated: false };
    if (visitedDirs >= maxDirs || results.length >= maxResults) {
      truncated = true;
      break;
    }
    const dir = queue.shift()!;
    visitedDirs++;
    let entries: DirEntry[];
    try { entries = (await fs.listDir(dir)) as DirEntry[]; } catch { continue; }
    for (const entry of entries) {
      if (!options.showHidden && entry.name.startsWith(".")) continue;
      const child = joinPath(dir, entry.name);
      if (entry.isDir) {
        if (!IGNORED_DIRS.has(entry.name) && !(dir === "/" && PSEUDO_ROOT_DIRS.has(entry.name))) queue.push(child);
        continue;
      }
      if (cancelled()) return { results: [], truncated: false };
      try {
        const meta = await fs.fileMeta(child) as { size?: number; isBinary?: boolean };
        if (meta.isBinary || (meta.size ?? 0) > maxFileBytes) continue;
        const content = decode((await fs.readFile(child, maxFileBytes) as { data: unknown }).data);
        if (content === null) continue;
        const lines = content.split(/\r?\n/);
        for (let i = 0; i < lines.length; i++) {
          if (!matcher(lines[i])) continue;
          results.push({ path: child, name: entry.name, line: i + 1, preview: lines[i].trim().slice(0, 240) });
          break;
        }
        if (results.length >= maxResults) { truncated = true; break; }
      } catch { /* unreadable/binary files are skipped */ }
    }
  }
  return { results, truncated };
}
