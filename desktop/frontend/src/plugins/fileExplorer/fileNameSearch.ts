import type { FileSystemBridge } from "./fsBridge";

export interface FileNameSearchResult {
  path: string;
  name: string;
}

export interface FileNameSearchOptions {
  showHidden: boolean;
  maxResults?: number;
  maxDirs?: number;
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
  const results: FileNameSearchResult[] = [];
  const queue = [root];
  let visitedDirs = 0;
  let truncated = false;

  while (queue.length > 0) {
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
