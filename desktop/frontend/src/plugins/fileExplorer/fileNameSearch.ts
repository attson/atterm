import type { FileSystemBridge } from "./fsBridge";

export interface FileNameSearchResult {
  path: string;
  name: string;
}

export interface FileNameSearchOptions {
  showHidden: boolean;
  maxResults?: number;
  maxDirs?: number;
  /** Asked before every listing. Returning true stops the walk immediately.
   *  Without it a superseded search keeps issuing listings for a query nobody
   *  is waiting for — free on a local disk, a round trip each on a remote
   *  one, and the caller discarding the result does not stop the traffic. */
  isCancelled?: () => boolean;
}

export interface FileNameSearchResponse {
  results: FileNameSearchResult[];
  truncated: boolean;
}

interface DirEntry {
  name: string;
  isDir: boolean;
}

const DEFAULT_MAX_RESULTS = 200;
const DEFAULT_MAX_DIRS = 2_000;
const IGNORED_DIRS = new Set([
  ".git",
  "node_modules",
  "dist",
  "build",
  ".next",
  ".turbo",
  "vendor",
]);

// Kernel-backed directory trees, skipped only when they sit directly on the
// filesystem root — which is where they are on Linux, and where an SSH source
// starts. /proc alone is thousands of entries that are not files anybody is
// searching for, and on a remote source each one costs a round trip. The
// check is anchored to "/" so a project directory called proc/ or dev/ is
// still searched.
const PSEUDO_ROOT_DIRS = new Set(["proc", "sys", "dev", "run"]);

function joinPath(parent: string, name: string): string {
  return parent.endsWith("/") ? parent + name : parent + "/" + name;
}

function isHidden(name: string): boolean {
  return name.startsWith(".");
}

export async function searchFileNames(
  fs: FileSystemBridge,
  root: string,
  query: string,
  options: FileNameSearchOptions,
): Promise<FileNameSearchResponse> {
  const q = query.trim().toLowerCase();
  if (!q) return { results: [], truncated: false };

  const maxResults = options.maxResults ?? DEFAULT_MAX_RESULTS;
  const maxDirs = options.maxDirs ?? DEFAULT_MAX_DIRS;
  const isCancelled = options.isCancelled ?? (() => false);
  const results: FileNameSearchResult[] = [];
  const queue = [root];
  let visitedDirs = 0;
  let truncated = false;

  while (queue.length > 0) {
    if (isCancelled()) return { results: [], truncated: false };
    if (visitedDirs >= maxDirs || results.length >= maxResults) {
      truncated = true;
      break;
    }
    const dir = queue.shift()!;
    visitedDirs++;
    let entries: DirEntry[];
    try {
      entries = (await fs.listDir(dir)) as DirEntry[];
    } catch {
      continue;
    }

    for (const entry of entries) {
      if (!options.showHidden && isHidden(entry.name)) continue;
      const childPath = joinPath(dir, entry.name);
      if (entry.isDir) {
        if (IGNORED_DIRS.has(entry.name)) continue;
        if (dir === "/" && PSEUDO_ROOT_DIRS.has(entry.name)) continue;
        queue.push(childPath);
        continue;
      }
      if (!entry.name.toLowerCase().includes(q)) continue;
      results.push({ path: childPath, name: entry.name });
      if (results.length >= maxResults) {
        truncated = true;
        break;
      }
    }
  }

  return { results, truncated };
}
