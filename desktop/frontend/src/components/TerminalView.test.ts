import { describe, expect, test } from "vitest";
import source from "./TerminalView.vue?raw";
import paneSource from "./PaneGrid.vue?raw";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("TerminalView overlay placement", () => {
  test("remote panes move attach progress below the remote badge", () => {
    expect(source).toContain("avoidTopRightBadge?: boolean");
    expect(source).toContain("avoidTopRightBadge");
    expect(paneSource).toContain(":avoid-top-right-badge=\"pane.remote\"");

    const offsetStyle = styleBlockFor(".overlay.avoid-top-right-badge");
    expect(offsetStyle).toMatch(/top\s*:\s*34px/);
  });
});

describe("TerminalView fit geometry", () => {
  test("puts terminal padding on xterm element so FitAddon subtracts it", () => {
    const termStyle = styleBlockFor(".term");
    expect(termStyle).not.toMatch(/padding\s*:/);
    expect(source).toMatch(/:deep\(\.xterm\)\s*\{[^}]*padding\s*:\s*6px 8px/);
  });
});

describe("TerminalView themes", () => {
  test("accepts a theme prop and uses it when creating xterm", () => {
    expect(source).toContain("theme: ITheme");
    expect(source).toMatch(/new Terminal\(\{[\s\S]*theme:\s*props\.theme/);
  });

  test("updates existing terminals without recreating the connection", () => {
    expect(source).toMatch(/watch\(\s*\(\)\s*=>\s*props\.theme/);
    expect(source).toContain("term.options.theme = theme");
    expect(source).not.toContain("watch(\n  () => props.theme,\n  () => {\n    ensureTerm()");
  });

  test("uses theme variables for terminal backgrounds", () => {
    expect(styleBlockFor(".term-view")).toMatch(/background\s*:\s*var\(--terminal-bg\)/);
    expect(source).toMatch(/background:\s*var\(--terminal-overlay\)/);
  });
});

describe("TerminalView right-click menu", () => {
  test("accepts remote permission and renders a teleported context menu", () => {
    expect(source).toContain("remotePermission?: string");
    expect(source).toContain('@contextmenu.prevent="openContextMenu"');
    expect(source).toContain('Teleport to="body"');
    expect(source).toContain('class="term-context-menu"');
    expect(source).toContain('emit("toast", result.reason');
    expect(paneSource).toContain(':remote-permission="sessionInfoFor(pane)?.remote_permission"');
  });

  test("renders copy/paste buttons with disabled state bindings", () => {
    expect(source).toContain('class="term-context-item"');
    expect(source).toContain(">copy<");
    expect(source).toContain(">paste<");
    expect(source).toContain(':disabled="!menuHasSelection"');
    expect(source).toContain(':disabled="!menuCanPaste || pasteBusy"');
  });

  test("uses fixed-position menu styling so overflow-hidden panes do not clip it", () => {
    const menuStyle = styleBlockFor(".term-context-menu");
    expect(menuStyle).toMatch(/position\s*:\s*fixed/);
    expect(menuStyle).toMatch(/z-index\s*:/);
  });

  test("renders a clear buffer menu item wired to onMenuClear", () => {
    expect(source).toContain(">clear buffer<");
    expect(source).toContain('@click="onMenuClear"');
    expect(source).toMatch(/function\s+onMenuClear\s*\(\s*\)/);
    expect(source).toContain("term.clear()");
    expect(source).toMatch(/const\s+MENU_HEIGHT\s*=\s*110/);
  });
});
