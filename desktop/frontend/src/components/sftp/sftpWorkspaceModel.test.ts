import { describe, expect, it } from "vitest";
import {
  buildTransferRequest,
  createPaneState,
  goBack,
  goForward,
  navigate,
  switchSource,
  visibleEntries,
  type SftpPaneState,
} from "./sftpWorkspaceModel";
import type { SFTPDirEntry } from "../../lib/api/ssh";

describe("SFTP workspace pane model", () => {
  it("keeps each pane history independent and supports back/forward", () => {
    const left = createPaneState({ kind: "local" }, "/Users/me");
    const right = createPaneState({ kind: "ssh", hostId: "remote" }, "/home/remote");

    const leftAtProject = navigate(left, "/Users/me/project");
    expect(leftAtProject.cwd).toBe("/Users/me/project");
    expect(leftAtProject.back).toEqual(["/Users/me"]);
    expect(right).toEqual(createPaneState({ kind: "ssh", hostId: "remote" }, "/home/remote"));

    const leftBack = goBack(leftAtProject);
    expect(leftBack.cwd).toBe("/Users/me");
    expect(leftBack.forward).toEqual(["/Users/me/project"]);
    expect(goForward(leftBack).cwd).toBe("/Users/me/project");
  });

  it("resets navigation, selection, and query when the source changes", () => {
    const state: SftpPaneState = {
      ...navigate(createPaneState({ kind: "local" }, "/Users/me"), "/Users/me/project"),
      selectedPaths: new Set(["/Users/me/project/a"]),
      query: "needle",
      sort: "size",
      direction: "desc",
    };

    const changed = switchSource(state, { kind: "ssh", hostId: "build" }, "/srv");
    expect(changed).toEqual({
      source: { kind: "ssh", hostId: "build" },
      cwd: "/srv",
      back: [],
      forward: [],
      selectedPaths: new Set(),
      query: "",
      showHidden: false,
      sort: "name",
      direction: "asc",
    });
  });

  it("filters case-insensitively and keeps directories first in both directions", () => {
    const entries: SFTPDirEntry[] = [
      { name: "z-file", isDir: false, size: 5, modTime: 20 },
      { name: "Alpha-dir", isDir: true, size: 0, modTime: 10 },
      { name: "beta-dir", isDir: true, size: 0, modTime: 30 },
      { name: "a-file", isDir: false, size: 50, modTime: 40 },
    ];

    const asc = visibleEntries(entries, createPaneState({ kind: "local" }, "/"));
    expect(asc.map((entry) => entry.name)).toEqual(["Alpha-dir", "beta-dir", "a-file", "z-file"]);

    const desc = visibleEntries(entries, {
      ...createPaneState({ kind: "local" }, "/"),
      direction: "desc",
    });
    expect(desc.map((entry) => entry.name)).toEqual(["beta-dir", "Alpha-dir", "z-file", "a-file"]);

    const bySize = visibleEntries(entries, {
      ...createPaneState({ kind: "local" }, "/"),
      sort: "size",
      direction: "desc",
    });
    expect(bySize.map((entry) => entry.name)).toEqual(["Alpha-dir", "beta-dir", "a-file", "z-file"]);

    const filtered = visibleEntries(entries, {
      ...createPaneState({ kind: "local" }, "/"),
      query: "ALPHA",
    });
    expect(filtered.map((entry) => entry.name)).toEqual(["Alpha-dir"]);
    expect(entries.map((entry) => entry.name)).toEqual(["z-file", "Alpha-dir", "beta-dir", "a-file"]);
  });

  it("hides dotfiles by default while preserving parent navigation entries", () => {
    const entries: SFTPDirEntry[] = [
      { name: "..", isDir: true },
      { name: ".env", isDir: false },
      { name: ".config", isDir: true },
      { name: "README.md", isDir: false },
    ];
    const state = createPaneState({ kind: "local" }, "/home/me");

    expect(state).toMatchObject({ showHidden: false });
    expect(visibleEntries(entries, state).map((entry) => entry.name)).toEqual(["..", "README.md"]);
    expect(visibleEntries(entries, { ...state, showHidden: true }).map((entry) => entry.name)).toEqual([
      "..",
      ".config",
      ".env",
      "README.md",
    ]);
  });
});

describe("SFTP workspace transfer requests", () => {
  it("preserves selection order and snapshots both endpoint identities", () => {
    const from: SftpPaneState = {
      ...createPaneState({ kind: "ssh", hostId: "left" }, "/src"),
      selectedPaths: new Set(["/src/b", "/src/a"]),
    };
    const to = createPaneState({ kind: "ssh", hostId: "right" }, "/dst");

    expect(buildTransferRequest(from, to, "rename")).toEqual({
      source: { kind: "ssh", host_id: "left" },
      destination: { kind: "ssh", host_id: "right" },
      paths: ["/src/b", "/src/a"],
      destination_dir: "/dst",
      conflict: "rename",
      accepted_host_key: { host: "", fingerprint: "" },
    });
  });

  it("rejects an empty selection, the same target, and a descendant target", () => {
    const empty = createPaneState({ kind: "local" }, "/tmp");
    expect(() => buildTransferRequest(empty, empty, "skip")).toThrow(/no files selected/i);

    const sameSource: SftpPaneState = {
      ...createPaneState({ kind: "ssh", hostId: "same" }, "/src"),
      selectedPaths: new Set(["/src/a"]),
    };
    expect(() => buildTransferRequest(sameSource, sameSource, "skip")).toThrow(/same path/i);

    const directory: SftpPaneState = {
      ...createPaneState({ kind: "ssh", hostId: "same" }, "/"),
      selectedPaths: new Set(["/tree"]),
    };
    const descendant = createPaneState({ kind: "ssh", hostId: "same" }, "/tree/child");
    expect(() => buildTransferRequest(directory, descendant, "skip")).toThrow(/descendant/i);
  });
});
