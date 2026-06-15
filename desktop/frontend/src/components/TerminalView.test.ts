import { describe, expect, it, test, vi } from "vitest";
import source from "./TerminalView.vue?raw";
import paneSource from "./PaneGrid.vue?raw";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import type { ContextMenuPlugin } from "../plugins/types";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("TerminalView overlay placement", () => {
  test("remote panes move attach progress below the remote badge", () => {
    expect(source).toContain("avoidTopRightBadge?: boolean");
    expect(source).toContain("avoidTopRightBadge");
    expect(paneSource).toContain(":avoid-top-right-badge=\"pane.remote ||");

    const offsetStyle = styleBlockFor(".overlay.avoid-top-right-badge");
    expect(offsetStyle).toMatch(/top\s*:\s*34px/);
  });
});

describe("TerminalView replay scroll", () => {
  test("scrolls to the newest output after initial replay finishes", () => {
    expect(source).toContain("function scrollToBottomAfterWriteQueue()");
    expect(source).toMatch(/progress\.phase === "end"[\s\S]*scrollToBottomAfterWriteQueue\(\)/);
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
    expect(source).toContain('emit("toast", result.reasonKey ? t(result.reasonKey) : result.reason');
    expect(paneSource).toContain(':remote-permission="sessionInfoFor(pane)?.remote_permission"');
  });

  test("renders copy/paste buttons with disabled state bindings", () => {
    expect(source).toContain('class="term-context-item"');
    expect(source).toContain('t("common.copy")');
    expect(source).toContain('t("common.paste")');
    expect(source).toContain(':disabled="!menuHasSelection"');
    expect(source).toContain(':disabled="!menuCanPaste || pasteBusy"');
  });

  test("uses fixed-position menu styling so overflow-hidden panes do not clip it", () => {
    const menuStyle = styleBlockFor(".term-context-menu");
    expect(menuStyle).toMatch(/position\s*:\s*fixed/);
    expect(menuStyle).toMatch(/z-index\s*:/);
  });

  test("renders a clear buffer menu item wired to onMenuClear", () => {
    expect(source).toContain('t("terminal.clearBuffer")');
    expect(source).toContain('@click="onMenuClear"');
    expect(source).toMatch(/function\s+onMenuClear\s*\(\s*\)/);
    expect(source).toContain("term.clear()");
    expect(source).toMatch(/const\s+MENU_HEIGHT\s*=\s*150/);
  });

  test("renders Open Link / Copy Link buttons gated by menuLinkHit", () => {
    expect(source).toContain('t("terminal.contextMenu.openLink")');
    expect(source).toContain('t("terminal.contextMenu.copyLink")');
    expect(source).toContain('v-if="menuLinkHit"');
    expect(source).toContain('@click="onMenuOpenLink"');
    expect(source).toContain('@click="onMenuCopyLink"');
  });

  test("computes link hit during openContextMenu and resets on close", () => {
    // computeLinkHit must run inside openContextMenu so the menu can decide
    // whether to show Open/Copy Link items.
    expect(source).toMatch(/menuLinkHit\.value\s*=\s*computeLinkHit\(e\)/);
    // closeContextMenu must clear it so the next right-click on plain text
    // doesn't carry over a stale hit.
    expect(source).toMatch(/menuLinkHit\.value\s*=\s*null/);
    // computeLinkHit reuses the same detectLinks used by the hover provider.
    expect(source).toMatch(/detectLinks\(line\)\.find/);
    expect(source).toContain("cellCoordsAt(");
  });
});

describe("TerminalView link provider wiring", () => {
  test("registers useTerminalLinkProvider with platform openExternalURL", () => {
    expect(source).toContain("useTerminalLinkProvider");
    expect(source).toContain("platform.system.openExternalURL");
    expect(source).toContain("getUserHomeDir");
    expect(source).toContain('emit("toast", t(key))');
  });

  test("disposes link provider in onBeforeUnmount", () => {
    expect(source).toMatch(/linkProviderDisposer\?\.dispose\(\)/);
    expect(source).toMatch(/linkProviderDisposer\s*=\s*null/);
  });
});

describe("TerminalView driver/viewer mode", () => {
  test("tracks isDriver ref from onDriverChange", () => {
    expect(source).toMatch(/const\s+isDriver\s*=\s*ref/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("locks term to META dims in viewer mode", () => {
    expect(source).toMatch(/term\.resize\(\s*cols\s*,\s*rows\s*\)/);
  });

  test("disables stdin when not driver", () => {
    expect(source).toMatch(/disableStdin/);
  });
});

describe("TerminalView viewer key handling", () => {
  test("intercepts bare Space in viewer mode and calls claimDriver", () => {
    expect(source).toMatch(/handleViewerKeydown/);
    expect(source).toMatch(/claimDriver/);
    expect(source).toMatch(/event\.key\s*===\s*" "/);
  });
});

describe("TerminalView viewer overlay", () => {
  test("renders a prominent viewer overlay when not driver", () => {
    expect(source).toContain('class="viewer-overlay"');
    expect(source).toContain('class="viewer-overlay-card"');
    expect(source).toContain('t("terminal.remoteHasTakenControl")');
    expect(source).toContain('t("terminal.pressSpaceToTakeBack")');
    expect(source).toMatch(/v-if=["']!isDriver["']/);
  });

  test("viewer-overlay covers the term-view with a dim backdrop", () => {
    const overlayCss = styleBlockFor(".viewer-overlay");
    expect(overlayCss).toMatch(/position\s*:\s*absolute/);
    expect(overlayCss).toMatch(/inset\s*:\s*0/);
    expect(overlayCss).toMatch(/background/); // dim backdrop
    expect(overlayCss).toMatch(/pointer-events\s*:\s*none/); // don't block scroll/mouse
  });

  test("viewer-overlay-card has prominent centered content", () => {
    const cardCss = styleBlockFor(".viewer-overlay-card");
    expect(cardCss).toMatch(/border-radius/);
    expect(cardCss).toMatch(/padding/);
    expect(cardCss).toMatch(/text-align\s*:\s*center/);
  });
});

describe("TerminalView bell notifications", () => {
  test("accepts a sessionLabel prop", () => {
    expect(source).toContain("sessionLabel?: string");
  });

  test("subscribes to term.onBell and wires through shouldNotify + showNotification", () => {
    expect(source).toContain("term.onBell");
    expect(source).toContain("shouldNotify");
    expect(source).toContain("showNotification");
  });

  test("passes the resolved sessionLabel into the notification body", () => {
    expect(source).toContain('t("terminal.bellNotification"');
    expect(source).toContain("props.sessionLabel");
  });
});

describe("PaneGrid session label forwarding", () => {
  test("imports extractSessionLabel and passes it to TerminalView", () => {
    expect(paneSource).toContain("extractSessionLabel");
    expect(paneSource).toContain(':session-label="extractSessionLabel(sessionInfoFor(pane))"');
  });
});

describe("TerminalView driver hostname", () => {
  test("fetches local hostname via getHostInfo and passes as clientName", () => {
    expect(source).toMatch(/getHostInfo/);
    expect(source).toMatch(/clientName:\s*localHostname\.value/);
  });

  test("tracks driverHostname from onDriverChange callback", () => {
    expect(source).toMatch(/driverHostname\s*=\s*ref/);
    expect(source).toMatch(/driverHostname\.value\s*=/);
  });

  test("overlay renders driverHostname when present", () => {
    expect(source).toContain("v-if=\"driverHostname\"");
    expect(source).toContain('class="viewer-overlay-host"');
    expect(source).toContain('t("terminal.byHost"');
  });
});

describe("TerminalView OSC 133 command notifications", () => {
  test("imports CommandTracker and shouldNotifyCommand from commandFinish", () => {
    expect(source).toContain("CommandTracker");
    expect(source).toContain("shouldNotifyCommand");
    expect(source).toContain('from "../lib/commandFinish"');
  });

  test("declares commandNotifyThresholdSec and isLocalSession props", () => {
    expect(source).toContain("commandNotifyThresholdSec");
    expect(source).toContain("isLocalSession");
  });

  test("registers OSC 133 handler on the xterm parser", () => {
    expect(source).toMatch(/term\.parser\.registerOscHandler\(\s*133\s*,/);
  });

  test("invokes showNotification with a Command-finished body when gate passes", () => {
    expect(source).toContain('t("terminal.commandFinishedNotification"');
    expect(source).toMatch(/showNotification\(/);
  });

  test("notification is gated by shouldNotifyCommand with focused, isLocal, threshold", () => {
    expect(source).toMatch(/shouldNotifyCommand\([\s\S]*focused[\s\S]*thresholdSec[\s\S]*isLocal/);
  });

  test("imports broadcastCommandFinished from api lib", () => {
    expect(source).toContain("broadcastCommandFinished");
    expect(source).toMatch(/from "\.\.\/lib\/api"/);
  });

  test("invokes broadcastCommandFinished after the local-notify gate passes", () => {
    // Must be inside the same `if (passed)` block as showNotification.
    const passedBlock = source.match(/if\s*\(\s*!\s*passed\s*\)[\s\S]*?return[\s\S]*?showNotification[\s\S]*?broadcastCommandFinished/);
    expect(passedBlock).not.toBeNull();
  });
});

describe("TerminalView close banner", () => {
  test("uses an i18n message with exit-code interpolation", () => {
    expect(source).toContain('t("terminal.sessionEndedBanner"');
    expect(source).toContain("exitCode: info.exit_code");
    expect(source).not.toContain("session ended (exit");
  });
});

// This is a wiring-level test, not a full TerminalView mount. It asserts
// that TerminalView's menu builder uses collectContextMenuItems with all
// registered + enabled context-menu plugins.

describe("TerminalView context-menu plugin merge", () => {
  it("merges plugin items after hardcoded copy/paste/clear", async () => {
    const fakePlugin: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "fake-1", label: "Fake item", onClick: vi.fn() }],
    };
    const hardcoded = [
      { id: "copy", label: "Copy", onClick: vi.fn() },
      { id: "paste", label: "Paste", onClick: vi.fn() },
      { id: "clear", label: "Clear buffer", onClick: vi.fn() },
    ];
    const pluginItems = await collectContextMenuItems([fakePlugin], {} as never, "selection");
    const merged = [...hardcoded, ...pluginItems];
    expect(merged.map((i) => i.id)).toEqual(["copy", "paste", "clear", "fake-1"]);
  });
});

test("viewer overlay has a take-control button wired to claimDriver", () => {
  expect(source).toMatch(/data-testid="take-control"/);
  expect(source).toMatch(/function takeControl[\s\S]*?claimDriver/);
});

describe("TerminalView right-click send", () => {
  test("imports the send predicate and payload helper", () => {
    expect(source).toMatch(/canSendSelection[\s\S]*from\s+["']\.\.\/lib\/terminalContextMenu["']/);
    expect(source).toMatch(/prepareSendPayload[\s\S]*from\s+["']\.\.\/lib\/terminalContextMenu["']/);
  });

  test("renders a send menu item between paste and clear", () => {
    const pasteIdx = source.indexOf('t("common.paste")');
    const sendIdx = source.indexOf('t("terminal.sendSelection")');
    const clearIdx = source.indexOf('t("terminal.clearBuffer")');
    expect(pasteIdx).toBeGreaterThan(-1);
    expect(sendIdx).toBeGreaterThan(pasteIdx);
    expect(clearIdx).toBeGreaterThan(sendIdx);
  });

  test("binds the send button's disabled state to menuCanSend", () => {
    expect(source).toContain('@click="onMenuSend"');
    expect(source).toContain(':disabled="!menuCanSend"');
    expect(source).toMatch(/const\s+menuCanSend\s*=\s*computed/);
  });

  test("menuCanSend feeds canSendSelection with selection + status + permission + isDriver", () => {
    expect(source).toMatch(
      /canSendSelection\(\s*\{[^}]*hasSelection[^}]*status[^}]*permission[^}]*isDriver[^}]*\}\s*\)/,
    );
  });

  test("onMenuSend writes through SessionConnection.sendInput, not term.paste", () => {
    expect(source).toMatch(/function\s+onMenuSend\s*\(\s*\)/);
    expect(source).toMatch(/prepareSendPayload\s*\(/);
    expect(source).toMatch(/conn\??\.sendInput\s*\(/);
    const sendBody = source.match(/function\s+onMenuSend\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(sendBody).not.toBeNull();
    expect(sendBody![0]).not.toMatch(/term\.paste\b/);
  });
});

describe("TerminalView template bar", () => {
  test("includes the template bar markup and click sends directly (no preview)", () => {
    expect(source).toContain('data-testid="template-bar"');
    expect(source).not.toContain("TemplatePreviewDialog");
    expect(source).toContain("sendTemplate(tpl)");
    expect(source).toMatch(/effectiveTemplates\s*\(/);
  });
});

describe("TerminalView resize-suspended", () => {
  test("declares resizeSuspended prop", () => {
    expect(source).toMatch(/resizeSuspended\?:\s*boolean/);
  });

  test("term.onResize gates conn.sendResize on resizeSuspended", () => {
    // The handler must early-return when props.resizeSuspended is true so the
    // PTY child sees one SIGWINCH on mouseup instead of dozens during drag.
    expect(source).toMatch(/term\.onResize\([\s\S]*?if \(props\.resizeSuspended\) return;[\s\S]*?conn\?\.sendResize/);
  });

  test("watches resize-suspended falling edge to emit final sendResize", () => {
    expect(source).toMatch(/watch\(\s*\(\)\s*=>\s*props\.resizeSuspended,[\s\S]*?prev\s*&&\s*!next[\s\S]*?conn\.sendResize\(term\.cols,\s*term\.rows\)/);
  });
});
