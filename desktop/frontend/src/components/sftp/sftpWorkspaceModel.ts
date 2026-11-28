import { ACCEPT_NO_HOST_KEY } from "../../lib/api/ssh";
import type {
  FileEndpoint,
  FileTransferConflict,
  SFTPDirEntry,
  StartFileTransferReq,
} from "../../lib/api/ssh";

export type SftpSource = { kind: "local" } | { kind: "ssh"; hostId: string };

export interface SftpPaneState {
  source: SftpSource;
  cwd: string;
  back: string[];
  forward: string[];
  selectedPaths: Set<string>;
  query: string;
  showHidden: boolean;
  sort: "name" | "modified" | "size";
  direction: "asc" | "desc";
}

export function createPaneState(source: SftpSource, cwd: string): SftpPaneState {
  return {
    source: cloneSource(source),
    cwd,
    back: [],
    forward: [],
    selectedPaths: new Set(),
    query: "",
    showHidden: false,
    sort: "name",
    direction: "asc",
  };
}

export function switchSource(_state: SftpPaneState, source: SftpSource, cwd: string): SftpPaneState {
  return createPaneState(source, cwd);
}

export function navigate(state: SftpPaneState, cwd: string): SftpPaneState {
  if (cwd === state.cwd) return clonePaneState(state);
  return {
    ...clonePaneState(state),
    cwd,
    back: [...state.back, state.cwd],
    forward: [],
    selectedPaths: new Set(),
  };
}

export function goBack(state: SftpPaneState): SftpPaneState {
  if (state.back.length === 0) return clonePaneState(state);
  const cwd = state.back[state.back.length - 1];
  return {
    ...clonePaneState(state),
    cwd,
    back: state.back.slice(0, -1),
    forward: [state.cwd, ...state.forward],
    selectedPaths: new Set(),
  };
}

export function goForward(state: SftpPaneState): SftpPaneState {
  if (state.forward.length === 0) return clonePaneState(state);
  const [cwd, ...forward] = state.forward;
  return {
    ...clonePaneState(state),
    cwd,
    back: [...state.back, state.cwd],
    forward,
    selectedPaths: new Set(),
  };
}

export function visibleEntries(entries: SFTPDirEntry[], state: SftpPaneState): SFTPDirEntry[] {
  const query = state.query.trim().toLocaleLowerCase();
  const filtered = entries.filter((entry) => {
    if (!state.showHidden && isHiddenName(entry.name)) return false;
    return !query || entry.name.toLocaleLowerCase().includes(query);
  });
  return [...filtered].sort((left, right) => compareEntries(left, right, state));
}

export function isNavigationEntry(entry: Pick<SFTPDirEntry, "name">): boolean {
  return entry.name === "." || entry.name === "..";
}

export function selectablePaths(paths: Iterable<string>): string[] {
  return Array.from(paths).filter((path) => (
    path.split(/[\\/]+/).every((part) => part !== "." && part !== "..")
  ));
}

function isHiddenName(name: string): boolean {
  return name.startsWith(".") && !isNavigationEntry({ name });
}

function compareEntries(left: SFTPDirEntry, right: SFTPDirEntry, state: SftpPaneState): number {
  if (left.isDir !== right.isDir) return left.isDir ? -1 : 1;
  if (state.sort === "name") {
    return compareNames(left.name, right.name) * directionFactor(state.direction);
  }

  const leftValue = state.sort === "size" ? (left.size ?? 0) : (left.modTime ?? 0);
  const rightValue = state.sort === "size" ? (right.size ?? 0) : (right.modTime ?? 0);
  if (leftValue !== rightValue) {
    return (leftValue < rightValue ? -1 : 1) * directionFactor(state.direction);
  }
  return compareNames(left.name, right.name);
}

function compareNames(left: string, right: string): number {
  const foldedLeft = left.toLocaleLowerCase();
  const foldedRight = right.toLocaleLowerCase();
  if (foldedLeft < foldedRight) return -1;
  if (foldedLeft > foldedRight) return 1;
  if (left < right) return -1;
  if (left > right) return 1;
  return 0;
}

function directionFactor(direction: SftpPaneState["direction"]): number {
  return direction === "desc" ? -1 : 1;
}

export function buildTransferRequest(
  from: SftpPaneState,
  to: SftpPaneState,
  conflict: FileTransferConflict,
): StartFileTransferReq {
  const paths = selectablePaths(from.selectedPaths);
  if (paths.length === 0) throw new Error("No files selected");

  const source = sourceEndpoint(from.source);
  const destination = sourceEndpoint(to.source);
  if (endpointID(source) === endpointID(destination)) {
    for (const sourcePath of paths) {
      const normalizedSource = normalizePathForComparison(from.source, sourcePath);
      const normalizedDestination = normalizePathForComparison(to.source, to.cwd);
      const target = joinPathForComparison(to.source, normalizedDestination, baseName(normalizedSource));
      if (target === normalizedSource) {
        throw new Error(`Source and destination resolve to the same path: ${sourcePath}`);
      }
      if (normalizedDestination.startsWith(`${trimRootSuffix(normalizedSource)}/`)) {
        throw new Error(`Cannot copy a directory into its descendant: ${sourcePath}`);
      }
    }
  }

  return {
    source,
    destination,
    paths,
    destination_dir: to.cwd,
    conflict,
    accepted_host_key: { ...ACCEPT_NO_HOST_KEY },
  };
}

function sourceEndpoint(source: SftpSource): FileEndpoint {
  return source.kind === "local"
    ? { kind: "local" }
    : { kind: "ssh", host_id: source.hostId };
}

function endpointID(endpoint: FileEndpoint): string {
  return endpoint.kind === "local" ? "local" : `ssh:${endpoint.host_id ?? ""}`;
}

function normalizePathForComparison(source: SftpSource, value: string): string {
  const slashPath = source.kind === "ssh" ? value : value.replaceAll("\\", "/");
  const prefix = slashPath.startsWith("/") ? "/" : "";
  const parts: string[] = [];
  for (const part of slashPath.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  const normalized = prefix + parts.join("/");
  return normalized || prefix || ".";
}

function joinPathForComparison(source: SftpSource, base: string, name: string): string {
  return normalizePathForComparison(source, `${trimRootSuffix(base)}/${name}`);
}

function baseName(value: string): string {
  const trimmed = trimRootSuffix(value);
  const index = trimmed.lastIndexOf("/");
  return index >= 0 ? trimmed.slice(index + 1) : trimmed;
}

function trimRootSuffix(value: string): string {
  return value === "/" ? "" : value.replace(/\/+$/, "");
}

function cloneSource(source: SftpSource): SftpSource {
  return source.kind === "local" ? { kind: "local" } : { kind: "ssh", hostId: source.hostId };
}

function clonePaneState(state: SftpPaneState): SftpPaneState {
  return {
    ...state,
    source: cloneSource(state.source),
    back: [...state.back],
    forward: [...state.forward],
    selectedPaths: new Set(state.selectedPaths),
  };
}
