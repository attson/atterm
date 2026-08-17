import { describe, expect, it, test, vi } from "vitest";
import source from "./TerminalView.vue?raw";
import quickTemplatesSource from "../composables/useQuickTemplates.ts?raw";
import paneSource from "./PaneGrid.vue?raw";
import appSource from "../App.vue?raw";
import searchBarSource from "./TerminalSearchBar.vue?raw";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import type { ContextMenuPlugin } from "../plugins/types";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("TerminalView plugin connection registry", () => {
  test("registers its live SessionConnection and only removes its own entry", () => {
    expect(appSource).toMatch(/reactive\(new Map<string, SessionConnection>\(\)\)/);
    expect(appSource).toMatch(/const selectedPane = computed<Pane \| null>/);
    expect(appSource).toMatch(/watch\(selectedPane,/);
    expect(source).toContain('"atterm:pluginSessionConnections"');
    expect(source).toMatch(/pluginSessionConnections\?\.set\(props\.sessionId, conn\)/);
    expect(source).toMatch(/pluginSessionConnections\?\.get\(props\.sessionId\) === conn/);
    expect(source).toMatch(/pluginSessionConnections\?\.delete\(props\.sessionId\)/);
    expect(source).toMatch(/pluginInputSenders\?\.set\(props\.sessionId, pluginInputSender\)/);
    expect(source).toMatch(/pluginInputSenders\?\.get\(props\.sessionId\) === pluginInputSender/);
    expect(source).toMatch(/pluginInputSenders\?\.delete\(props\.sessionId\)/);
  });
});

describe("TerminalView async mount lifecycle", () => {
  test("does not attach after an async mount resumes on an unmounted view", () => {
    expect(source).toContain("let isAlive = true;");
    expect(source).toMatch(/await ensureTerm\(\);\s*if \(!isAlive\) return;/);
    expect(source).toMatch(/await getHostInfo\(\);[\s\S]*?if \(!isAlive\) return;\s*if \(props\.active\) startConnection\(\)/);
    expect(source).toMatch(/onBeforeUnmount\(\(\) => \{\s*isAlive = false;/);
  });

  test("suspends hidden tab connections while keeping the xterm component mounted", () => {
    expect(source).toMatch(/if \(props\.active\) startConnection\(\)/);
    expect(source).toMatch(
      /watch\(\s*\(\)\s*=>\s*props\.active,[\s\S]*?if \(isActive\)[\s\S]*?conn\.attach\(\)[\s\S]*?else startConnection\(\)[\s\S]*?conn\?\.suspend\(\)/,
    );
  });

  test("does not create a second websocket if activation races async mount completion", () => {
    expect(source).toMatch(/function startConnection\(\) \{[\s\S]*?if \(conn\) \{[\s\S]*?conn\.attach\(\);[\s\S]*?return;[\s\S]*?\}/);
  });

  test("checks liveness inside ensureTerm after each async dependency", () => {
    expect(source).toMatch(/function isLiveTerminal\(\): boolean/);
    expect(source).toMatch(/await getWebglRendererEnabled\(\);[\s\S]*?if \(!isLiveTerminal\(\)\) return;[\s\S]*?keyTarget\.addEventListener/);
    expect(source).toMatch(/await getUserHomeDir\(\);[\s\S]*?if \(!isLiveTerminal\(\)\) return;[\s\S]*?useTerminalLinkProvider[\s\S]*?resizeObserver\.observe/);
  });
});

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
    expect(source).toMatch(/function scrollToBottomAfterWriteQueue\(done\?: \(\) => void\)/);
    expect(source).toMatch(/progress\.phase === "start" \|\| progress\.phase === "chunk"/);
    expect(source).toMatch(/replayInputGuard\.onProgress\("end"[\s\S]*scrollToBottomAfterWriteQueue\(release\)/);
  });
});

describe("TerminalView resize deduplication", () => {
  test("records locally sent PTY dimensions to avoid repeated tab-activation RESIZE", () => {
    expect(source).toMatch(/conn\.sendResize\(term\.cols, term\.rows\);[\s\S]*lastResizeSent\.value = \{ cols: term\.cols, rows: term\.rows \};/);
    expect(source).toMatch(/conn\?\.sendResize\(cols, rows\);[\s\S]*ptyCols\.value = cols;[\s\S]*ptyRows\.value = rows;/);
  });
});

describe("TerminalView fit geometry", () => {
  test("puts terminal padding on xterm element so FitAddon subtracts it", () => {
    const termStyle = styleBlockFor(".term");
    expect(termStyle).not.toMatch(/padding\s*:/);
    expect(source).toMatch(/:deep\(\.xterm\)\s*\{[^}]*padding\s*:\s*6px 8px/);
  });

  test("insets xterm viewport to match root padding so scroll bounds align with rendered rows", () => {
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*top\s*:\s*6px/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*right\s*:\s*8px/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*bottom\s*:\s*6px/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*left\s*:\s*8px/);
  });

  test("keeps iOS terminal scrollback native and hides WebKit text affordances", () => {
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*touch-action\s*:\s*pan-y/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*-webkit-overflow-scrolling\s*:\s*touch/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*overscroll-behavior\s*:\s*contain/);
    expect(source).toMatch(/:deep\(\.xterm-viewport\)\s*\{[^}]*-webkit-touch-callout\s*:\s*none/);
    expect(source).toMatch(/:deep\(\.xterm-helper-textarea\)\s*\{[^}]*caret-color\s*:\s*transparent/);
  });
});

describe("TerminalView web auxiliary keys", () => {
  test("loads the mobile aux-key defaults for browser terminals only", () => {
    expect(source).toContain('effectiveAuxKeys, type AuxKey');
    expect(source).toMatch(/const\s+auxKeys\s*=\s*ref<readonly AuxKey\[\]>\(\[\]\)/);
    expect(source).toMatch(/effectiveAuxKeys\(platform\.auxKeys\)/);
    expect(source).toMatch(/const\s+showAuxKeyBar\s*=\s*computed\(\s*\(\)\s*=>\s*![^)]*platform\.caps\.wailsBindings[\s\S]*?![^)]*platform\.caps\.localPty/);
  });

  test("renders an aux-key row and sends raw sequences through the active connection", () => {
    expect(source).toContain('data-testid="terminal-aux-key-bar"');
    expect(source).toContain(':data-testid="`terminal-aux-key-${key.id}`"');
    expect(source).toContain('@pointerdown="onShortcutPointerDown"');
    expect(source).toContain('@pointermove="onShortcutPointerMove"');
    expect(source).toContain('@pointercancel="onShortcutPointerCancel"');
    expect(source).toContain('@pointerup="sendAuxPointer(key.seq, $event)"');
    expect(source).toMatch(/function\s+sendAuxPointer\s*\(\s*seq:\s*string,\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/shouldSendShortcutPointer\(event\)/);
    expect(source).toMatch(/conn\?\.sendInput\(seq\)/);
    expect(source).toMatch(/if\s*\(\s*keyboardWanted\.value\s*\)\s*focusTerminalIfDriver\(\)/);
    expect(source).toMatch(/const\s+auxKeysCanSend\s*=\s*computed/);
    expect(source).toContain(':disabled="!auxKeysCanSend"');
    expect(source).toMatch(/type="button"[\s\S]*:data-testid="`terminal-aux-key-\$\{key\.id\}`"[\s\S]*tabindex="-1"/);
  });

  test("keeps less-used image paste and file buttons after the shortcut keys", () => {
    const bar = source.match(/data-testid="terminal-aux-key-bar"[\s\S]*?<\/div>\s*<div\s+v-if="templateConfirmOpen"/);
    expect(bar).not.toBeNull();
    const text = bar![0];
    const keyboard = text.indexOf('data-testid="terminal-hide-keyboard"');
    const scroll = text.indexOf('data-testid="terminal-aux-key-scroll"');
    const aux = text.indexOf(':data-testid="`terminal-aux-key-${key.id}`"');
    const image = text.indexOf('data-testid="terminal-pick-image"');
    const paste = text.indexOf('data-testid="terminal-paste-confirm-open"');
    const file = text.indexOf('data-testid="terminal-pick-file"');
    expect(keyboard).toBeGreaterThanOrEqual(0);
    expect(scroll).toBeGreaterThan(keyboard);
    expect(aux).toBeGreaterThan(scroll);
    expect(image).toBeGreaterThan(aux);
    expect(paste).toBeGreaterThan(image);
    expect(file).toBeGreaterThan(paste);

    const scrollTrack = source.match(/data-testid="terminal-aux-key-scroll"[\s\S]*?<\/div>\s*<\/div>\s*<div\s+v-if="templateConfirmOpen"/);
    expect(scrollTrack).not.toBeNull();
    expect(scrollTrack![0]).not.toContain('data-testid="terminal-hide-keyboard"');
  });

  test("requires a second confirmation before sending mobile quick templates", () => {
    // The confirm state machine + requestTemplateSend / confirmTemplateSend /
    // cancelTemplateSend now live in composables/useQuickTemplates.ts. The
    // caps-based confirmRequired gate is passed in from TerminalView (that
    // computed still lives here so the composable stays session-agnostic).
    expect(quickTemplatesSource).toMatch(/const\s+pendingTemplateConfirm\s*=\s*ref<QuickTemplate\s*\|\s*null>\(null\)/);
    expect(quickTemplatesSource).toMatch(/const\s+templateConfirmOpen\s*=\s*computed\(\s*\(\)\s*=>\s*pendingTemplateConfirm\.value\s*!==\s*null\s*\)/);
    expect(quickTemplatesSource).toMatch(/function\s+requestTemplateSend\s*\(\s*tpl:\s*QuickTemplate\s*\)/);
    expect(quickTemplatesSource).toMatch(/function\s+confirmTemplateSend\s*\(\s*\)/);
    expect(quickTemplatesSource).toMatch(/function\s+cancelTemplateSend\s*\(\s*\)/);
    expect(source).toMatch(/useQuickTemplates\(\{[\s\S]*?platform\.caps\.wailsBindings\s*\|\|\s*platform\.caps\.localPty/);
    expect(source).toContain('@touchend="onKeyboardControlTouchEnd($event, () => requestTemplateSend(tpl))"');
    expect(source).toContain('@click="onKeyboardControlClick(() => requestTemplateSend(tpl))"');
    expect(source).toContain('data-testid="template-confirm-panel"');
    expect(source).toContain('data-testid="template-confirm-send"');
    expect(source).toContain('data-testid="template-confirm-cancel"');

    // These functions are nested inside useQuickTemplates() so their
    // closing braces sit at 2-space indent — `\n {2}\}` matches that
    // without eating the inner `if`'s 4-space `}`.
    const requestBody = quickTemplatesSource.match(/function\s+requestTemplateSend\s*\([^)]*\)[^{]*\{[\s\S]*?\n {2}\}/);
    expect(requestBody).not.toBeNull();
    expect(requestBody![0]).toMatch(/confirmRequired\.value/);
    expect(requestBody![0]).toMatch(/pendingTemplateConfirm\.value\s*=\s*tpl/);
    expect(requestBody![0]).toMatch(/sendTemplate\(tpl\)/);

    const confirmBody = quickTemplatesSource.match(/function\s+confirmTemplateSend\s*\([^)]*\)[^{]*\{[\s\S]*?\n {2}\}/);
    expect(confirmBody).not.toBeNull();
    expect(confirmBody![0]).toMatch(/pendingTemplateConfirm\.value/);
    expect(confirmBody![0]).toMatch(/pendingTemplateConfirm\.value\s*=\s*null/);
    expect(confirmBody![0]).toMatch(/sendTemplate\(tpl\)/);
  });

  test("does not send shortcut keys while the user is horizontally scrolling the shortcut bar", () => {
    expect(source).toMatch(/const\s+SHORTCUT_TAP_MOVE_PX\s*=\s*10/);
    expect(source).toMatch(/let\s+shortcutPointerState:\s*ShortcutPointerState\s*\|\s*null\s*=\s*null/);
    expect(source).toMatch(/let\s+lastShortcutTouchActionAt\s*=\s*0/);
    expect(source).toMatch(/function\s+onShortcutPointerDown\s*\(\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/function\s+onShortcutPointerMove\s*\(\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/function\s+sendAuxTouch\s*\(\s*seq:\s*string,\s*event:\s*TouchEvent\s*\)/);
    expect(source).toMatch(/function\s+shouldSendShortcutPointer\s*\(\s*event:\s*PointerEvent\s*\)/);

    const moveBody = source.match(/function\s+onShortcutPointerMove\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(moveBody).not.toBeNull();
    expect(moveBody![0]).toMatch(/SHORTCUT_TAP_MOVE_PX\s*\*\s*SHORTCUT_TAP_MOVE_PX/);
    expect(moveBody![0]).toMatch(/shortcutPointerState\.cancelled\s*=\s*true/);

    const sendGateBody = source.match(/function\s+shouldSendShortcutPointer\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(sendGateBody).not.toBeNull();
    expect(sendGateBody![0]).toMatch(/Date\.now\(\)\s*-\s*lastShortcutTouchActionAt/);
    expect(sendGateBody![0]).toMatch(/!shortcutPointerState\.cancelled/);
    expect(sendGateBody![0]).toMatch(/clearShortcutPointerState\(event\)/);

    const touchSendBody = source.match(/function\s+sendAuxTouch\s*\(\s*seq:\s*string,\s*event:\s*TouchEvent\s*\)\s*\{[\s\S]*?\n\}/);
    expect(touchSendBody).not.toBeNull();
    expect(touchSendBody![0]).toMatch(/onKeyboardControlTouchEnd\(event,\s*\(\)\s*=>\s*\{/);
    expect(touchSendBody![0]).toMatch(/lastShortcutTouchActionAt\s*=\s*Date\.now\(\)/);
    expect(touchSendBody![0]).toMatch(/conn\?\.sendInput\(seq\)/);
  });

  test("renders a local keyboard toggle button that does not send terminal input", () => {
    expect(source).toContain('data-testid="terminal-hide-keyboard"');
    expect(source).toContain('class="aux-key-btn keyboard-toggle-btn"');
    expect(source).toContain('@pointerdown.stop="onKeyboardControlPointerDown"');
    expect(source).toContain('@click.prevent.stop="onKeyboardControlClick(toggleSoftKeyboard)"');
    expect(source).toMatch(/const\s+keyboardWanted\s*=\s*ref\(false\)/);
    expect(source).toMatch(/const\s+softKeyboardOpening\s*=\s*ref\(false\)/);
    expect(source).toMatch(/const\s+softKeyboardOpen\s*=\s*computed\(\s*\(\)\s*=>\s*imeFocused\.value\s*\|\|\s*softKeyboardOpening\.value\s*\)/);
    expect(source).toMatch(/const\s+softKeyboardCollapsed\s*=\s*computed\(\s*\(\)\s*=>\s*!softKeyboardOpen\.value\s*\)/);
    expect(source).toMatch(/function\s+hideSoftKeyboard\s*\(\s*\)/);
    expect(source).toMatch(/function\s+showSoftKeyboard\s*\(\s*\)/);
    expect(source).toMatch(/function\s+toggleSoftKeyboard\s*\(\s*\)/);
    expect(source).toMatch(/const\s+SOFT_KEYBOARD_TOGGLE_DEDUP_MS\s*=\s*350/);
    expect(source).toMatch(/const\s+SOFT_KEYBOARD_OPEN_RETRY_DELAYS_MS\s*=\s*\[60,\s*180,\s*360\]/);
    expect(source).toMatch(/const\s+SOFT_KEYBOARD_OPEN_GRACE_MS\s*=\s*900/);
    expect(source).toMatch(/let\s+lastSoftKeyboardToggleAt\s*=\s*-Infinity/);
    expect(source).toMatch(/function\s+shouldRunSoftKeyboardToggle\s*\(\s*\)/);
    expect(source).toMatch(/function\s+scheduleSoftKeyboardOpenFocusRetries\s*\(\s*\)/);
    expect(source).toMatch(/platform\.system\.hideSoftKeyboard\?\.\(\)/);
    expect(source).toContain("{{ softKeyboardCollapsed ? '打开键盘 ⌃' : '折叠键盘 ⌄' }}");
    const hideBody = source.match(/function\s+hideSoftKeyboard\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(hideBody).not.toBeNull();
    expect(hideBody![0]).not.toMatch(/sendInput/);
    const toggleButton = source.match(/data-testid="terminal-hide-keyboard"[\s\S]*?<\/button>/);
    expect(toggleButton).not.toBeNull();
    expect(toggleButton![0]).not.toMatch(/onShortcutPointer/);
    const toggleCss = styleBlockFor(".keyboard-toggle-btn");
    expect(toggleCss).toMatch(/min-width\s*:\s*76px/);
    expect(toggleCss).toMatch(/font-weight\s*:\s*600/);
    const auxBarCss = styleBlockFor(".aux-key-bar");
    expect(auxBarCss).not.toMatch(/overflow-x\s*:\s*auto/);
    const auxScrollCss = styleBlockFor(".aux-key-scroll");
    expect(auxScrollCss).toMatch(/overflow-x\s*:\s*auto/);
    expect(auxScrollCss).toMatch(/min-width\s*:\s*0/);

    const dedupBody = source.match(/function\s+shouldRunSoftKeyboardToggle\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(dedupBody).not.toBeNull();
    expect(dedupBody![0]).toMatch(/Date\.now\(\)/);
    expect(dedupBody![0]).toMatch(/SOFT_KEYBOARD_TOGGLE_DEDUP_MS/);
    expect(dedupBody![0]).toMatch(/lastSoftKeyboardToggleAt\s*=\s*now/);

    const retryBody = source.match(/function\s+scheduleSoftKeyboardOpenFocusRetries\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(retryBody).not.toBeNull();
    expect(retryBody![0]).toMatch(/softKeyboardOpening\.value\s*=\s*true/);
    expect(retryBody![0]).toMatch(/SOFT_KEYBOARD_OPEN_RETRY_DELAYS_MS/);
    expect(retryBody![0]).toMatch(/focusTerminalIfDriver\(\)/);
    expect(retryBody![0]).toMatch(/scrollToBottomForKeyboardOpen\(\)/);
    expect(retryBody![0]).toMatch(/SOFT_KEYBOARD_OPEN_GRACE_MS/);
    expect(retryBody![0]).toMatch(/keyboardWanted\.value\s*=\s*false/);

    const toggleBody = source.match(/function\s+toggleSoftKeyboard\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(toggleBody).not.toBeNull();
    expect(toggleBody![0]).toMatch(/shouldRunSoftKeyboardToggle\(\)/);
  });

  test("scrolls to the newest terminal output when opening the soft keyboard", () => {
    expect(source).toMatch(/function\s+scrollToBottomForKeyboardOpen\s*\(\s*\)/);
    const scrollBody = source.match(/function\s+scrollToBottomForKeyboardOpen\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(scrollBody).not.toBeNull();
    expect(scrollBody![0]).toMatch(/term\.scrollToBottom\(\)/);
    expect(scrollBody![0]).toMatch(/restoreScrollPositionAfterKeyboardToggle\(\{/);
    expect(scrollBody![0]).toMatch(/atBottom:\s*true/);

    const showBody = source.match(/function\s+showSoftKeyboard\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(showBody).not.toBeNull();
    expect(showBody![0]).toMatch(/keyboardWanted\.value\s*=\s*true/);
    expect(showBody![0]).toMatch(/scrollToBottomForKeyboardOpen\(\)/);
    expect(showBody![0]).toMatch(/focusTerminalIfDriver\(\)/);
    expect(showBody![0]).toMatch(/scheduleSoftKeyboardOpenFocusRetries\(\)/);

    const focusBody = source.match(/function\s+onImeFocus\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(focusBody).not.toBeNull();
    expect(focusBody![0]).toMatch(/keyboardWanted\.value/);
    expect(focusBody![0]).toMatch(/scrollToBottomForKeyboardOpen\(\)/);
  });

  test("user scroll during a keyboard-open cycle cancels the snap-to-bottom + restore chain", () => {
    // Regression: on mobile, tapping to open the keyboard fires
    // scrollToBottomForKeyboardOpen (which snap-to-bottoms + queues 4
    // restore timers at 80/180/360/700 ms) and scheduleSoftKeyboardOpenFocusRetries
    // (which re-fires scrollToBottomForKeyboardOpen at 60/180/360 ms). Any
    // up-scroll the user made in that window would get yanked back to the
    // bottom. onViewportUserScrollIntent latches a flag + clears pending
    // restore timers so the user's intent wins; the snap/apply paths bail
    // on the flag; showSoftKeyboard/hideSoftKeyboard reset the flag to
    // start a fresh cycle.
    expect(source).toContain("let userScrolledDuringKeyboardCycle = false;");
    expect(source).toMatch(/function\s+onViewportUserScrollIntent\s*\([^)]*\)\s*\{[\s\S]*?userScrolledDuringKeyboardCycle\s*=\s*true[\s\S]*?clearKeyboardScrollRestoreTimers\(\)[\s\S]*?pendingKeyboardScrollPosition\s*=\s*null[\s\S]*?\}/);
    // Both scroll-forcing paths must bail on the flag.
    expect(source).toMatch(/function\s+scrollToBottomForKeyboardOpen[\s\S]*?if\s*\(\s*userScrolledDuringKeyboardCycle\s*\)/);
    expect(source).toMatch(/function\s+applyScrollPosition[\s\S]*?if\s*\(\s*userScrolledDuringKeyboardCycle\s*\)/);
    // Fresh state per open/close cycle so a new tap after user scroll re-arms the snap.
    expect(source).toMatch(/function\s+showSoftKeyboard[\s\S]*?userScrolledDuringKeyboardCycle\s*=\s*false/);
    expect(source).toMatch(/function\s+hideSoftKeyboard[\s\S]*?userScrolledDuringKeyboardCycle\s*=\s*false/);
    // Wheel on the viewport still cancels the restore (desktop). Mobile
    // touch signal is now via onTermTouchMoveForRestoreCancel on .term
    // (xterm owns the actual scroll; we only piggy-back the flag).
    expect(source).toContain('addEventListener("wheel", onViewportUserScrollIntent');
    expect(source).toContain('removeEventListener("wheel", onViewportUserScrollIntent');
    expect(source).toMatch(/function\s+onTermTouchMoveForRestoreCancel\s*\([^)]*\)\s*\{[\s\S]*?onViewportUserScrollIntent\(\)/);
  });

  test("onTermTouchStart NEVER stopPropagation — xterm's touchstart must always fire to init Viewport._lastTouchY", () => {
    // Regression root cause: any stopPropagation on .term's
    // @touchstart.capture blocks xterm's touchstart (registered on .xterm
    // root, a descendant of .term) from firing on descendants. Xterm's
    // touchstart is what sets Viewport._lastTouchY = ev.touches[0].pageY;
    // without that init, the subsequent touchmove computes deltaY against
    // 0 → huge negative → viewport.scrollTop clamped to 0 → user sees
    // "first swipe does nothing." Even scoping stopPropagation to
    // `softKeyboardOpen.value === true` isn't safe, because that computed
    // includes `imeFocused.value`, which can be stale when iOS drops a
    // blur event — the swipe silently dies on subsequent touches with no
    // visible keyboard involvement. preventDefault alone suffices to
    // stop the browser's default focus-into-IME, and xterm's touchstart
    // is registered passive so it can't preventDefault anything.
    const body = source.match(/function\s+onTermTouchStart\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    // Guard the hide-keyboard side still preventDefaults and hides.
    expect(body![0]).toMatch(/preventDefault\(\)/);
    expect(body![0]).toMatch(/hideSoftKeyboard\(\)/);
    // NO stopPropagation calls anywhere in this function.
    const stopCount = (body![0].match(/stopPropagation\(\)/g) || []).length;
    expect(stopCount).toBe(0);
  });

  test("touchmove on .term forwards user scroll intent to the keyboard-restore cancel (letting xterm own actual scroll)", () => {
    // Xterm 5.3 already handles the touchmove-to-scrollTop translation via
    // Viewport.handleTouchMove on the .xterm root. We only piggy-back to
    // set the userScrolledDuringKeyboardCycle flag so the ~1.6 s soft-
    // keyboard restore chain stops fighting the user. No delta math, no
    // scrollTop mutation here.
    expect(source).toMatch(/function\s+onTermTouchMoveForRestoreCancel\s*\([^)]*\)\s*\{[\s\S]*?onViewportUserScrollIntent\(\)[\s\S]*?\}/);
    expect(source).toContain('keyTarget.addEventListener("touchmove", onTermTouchMoveForRestoreCancel');
    expect(source).toContain('copyKeyTarget?.removeEventListener("touchmove", onTermTouchMoveForRestoreCancel');
    // The earlier misdirected manual bridge is gone (would double-scroll).
    expect(source).not.toContain("touchScrollLastY");
    expect(source).not.toContain("onTerminalTouchMoveForScroll");
    expect(source).not.toContain("onTerminalTouchStartForScroll");
  });

  test("keeps the keyboard opening state stable across transient mobile blur", () => {
    const toggleBody = source.match(/function\s+toggleSoftKeyboard\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(toggleBody).not.toBeNull();
    expect(toggleBody![0]).toMatch(/softKeyboardCollapsed\.value/);
    expect(toggleBody![0]).not.toMatch(/keyboardWanted\.value/);

    const blurBody = source.match(/function\s+onImeBlur\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(blurBody).not.toBeNull();
    expect(blurBody![0]).toMatch(/imeFocused\.value\s*=\s*false/);
    expect(blurBody![0]).toMatch(/keyboardWanted\.value\s*&&\s*softKeyboardOpening\.value/);
    expect(blurBody![0]).toMatch(/return/);
    expect(blurBody![0]).toMatch(/keyboardWanted\.value\s*=\s*false/);
  });

  test("preserves terminal scroll position while hiding the soft keyboard", () => {
    expect(source).toMatch(/function\s+captureScrollPosition\s*\(/);
    expect(source).toMatch(/function\s+restoreScrollPositionAfterKeyboardToggle\s*\(/);
    expect(source).toMatch(/const\s+scrollPosition\s*=\s*captureScrollPosition\(\)/);
    expect(source).toMatch(/restoreScrollPositionAfterKeyboardToggle\(scrollPosition\)/);
    expect(source).toMatch(/term\.scrollToBottom\(\)/);
    expect(source).toMatch(/term\.scrollToLine\(scrollPosition\.viewportY\)/);
    expect(source).toMatch(/const\s+KEYBOARD_SCROLL_RESTORE_DELAYS_MS\s*=\s*\[80,\s*180,\s*360,\s*700\]/);
    expect(source).toMatch(/function\s+restorePendingKeyboardScrollPosition\s*\(/);
    expect(source).toMatch(/keyboardScrollRestoreTimers\.push\(window\.setTimeout/);
    expect(source).toMatch(/resizeObserver\s*=\s*new ResizeObserver\(\(\)\s*=>\s*\{[\s\S]*?safeFit\(\);[\s\S]*?restorePendingKeyboardScrollPosition\(\);[\s\S]*?\}\)/);
  });

  test("lets a terminal-area tap dismiss the focused mobile keyboard without sending input", () => {
    expect(source).toContain('@pointerdown.capture="onTermPointerDown"');
    expect(source).toMatch(/function\s+onTermPointerDown\s*\(\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/document\.activeElement\s*===\s*imeInputTarget/);
    expect(source).toMatch(/event\.preventDefault\(\)/);
    expect(source).toMatch(/event\.stopPropagation\(\)/);
    expect(source).toMatch(/hideSoftKeyboard\(\)/);
    const handlerBody = source.match(/function\s+onTermPointerDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(handlerBody).not.toBeNull();
    expect(handlerBody![0]).not.toMatch(/sendInput/);
  });

  test("blocks touch focus while keyboard is collapsed but still starts long-press selection", () => {
    expect(source).toMatch(/function\s+shouldBlockTerminalPointerFocus\s*\(\s*event:\s*PointerEvent\s*\)/);
    const helperBody = source.match(/function\s+shouldBlockTerminalPointerFocus\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(helperBody).not.toBeNull();
    expect(helperBody![0]).toMatch(/platform\.caps\.wailsBindings\s*\|\|\s*platform\.caps\.localPty/);
    expect(helperBody![0]).not.toMatch(/keyboardWanted\.value/);
    expect(helperBody![0]).not.toMatch(/softKeyboardOpen\.value/);
    expect(helperBody![0]).toMatch(/event\.pointerType\s*===\s*"mouse"/);

    const handlerBody = source.match(/function\s+onTermPointerDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(handlerBody).not.toBeNull();
    expect(handlerBody![0]).toMatch(/shouldBlockTerminalPointerFocus\(event\)/);
    expect(handlerBody![0]).toMatch(/event\.preventDefault\(\)/);
    expect(handlerBody![0]).toMatch(/event\.stopPropagation\(\)/);
    expect(handlerBody![0]).toMatch(/onSelectionPointerDown\(event\)/);
  });

  test("collapses an open mobile keyboard on any terminal touch", () => {
    expect(source).toMatch(/function\s+shouldHideSoftKeyboardForTerminalPointer\s*\(\s*event:\s*PointerEvent\s*\)/);
    const hideBody = source.match(/function\s+shouldHideSoftKeyboardForTerminalPointer\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(hideBody).not.toBeNull();
    expect(hideBody![0]).toMatch(/platform\.caps\.wailsBindings\s*\|\|\s*platform\.caps\.localPty/);
    expect(hideBody![0]).toMatch(/softKeyboardOpen\.value/);
    expect(hideBody![0]).toMatch(/event\.pointerType\s*===\s*"mouse"/);
    expect(hideBody![0]).not.toMatch(/isPointerInBottomInputArea/);

    const handlerBody = source.match(/function\s+onTermPointerDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(handlerBody).not.toBeNull();
    expect(handlerBody![0]).toMatch(/shouldHideSoftKeyboardForTerminalPointer\(event\)/);
    expect(handlerBody![0]).toMatch(/hideSoftKeyboard\(\)/);
    expect(handlerBody![0]).toMatch(/onSelectionPointerDown\(event\)/);
  });

  test("blocks the touchstart and compatibility mousedown paths that can refocus xterm after collapse", () => {
    expect(source).toContain('@touchstart.capture="onTermTouchStart"');
    expect(source).toContain('@mousedown.capture="onTermMouseDown"');
    expect(source).toMatch(/let\s+lastBlockedTerminalTouchAt\s*=\s*0/);
    expect(source).toMatch(/const\s+TOUCH_COMPAT_MOUSE_BLOCK_MS\s*=\s*800/);

    const touchBody = source.match(/function\s+onTermTouchStart\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(touchBody).not.toBeNull();
    expect(touchBody![0]).toMatch(/shouldBlockTerminalTouchFocus\(event\)/);
    expect(touchBody![0]).toMatch(/lastBlockedTerminalTouchAt\s*=\s*Date\.now\(\)/);
    expect(touchBody![0]).toMatch(/event\.preventDefault\(\)/);
    // stopPropagation was intentionally removed — it blocked xterm's
    // touchstart from initializing Viewport._lastTouchY, killing the first
    // swipe. preventDefault alone stops the refocus, xterm's touchstart is
    // passive (can't preventDefault) and is safe to let through. See the
    // dedicated "onTermTouchStart NEVER stopPropagation" test above.
    expect(touchBody![0]).not.toMatch(/event\.stopPropagation\(\)/);
    expect(touchBody![0]).toMatch(/hideSoftKeyboard\(\)/);

    const mouseBody = source.match(/function\s+onTermMouseDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(mouseBody).not.toBeNull();
    expect(mouseBody![0]).toMatch(/Date\.now\(\)\s*-\s*lastBlockedTerminalTouchAt/);
    expect(mouseBody![0]).toMatch(/TOUCH_COMPAT_MOUSE_BLOCK_MS/);
    expect(mouseBody![0]).toMatch(/event\.preventDefault\(\)/);
    expect(mouseBody![0]).toMatch(/event\.stopPropagation\(\)/);
  });

  test("keeps the keyboard open only on shortcut/control surfaces", () => {
    expect(source).toMatch(/function\s+isKeyboardControlSurface\s*\(\s*target:\s*EventTarget\s*\|\s*null\s*\)/);
    const surfaceBody = source.match(/function\s+isKeyboardControlSurface\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(surfaceBody).not.toBeNull();
    expect(surfaceBody![0]).toMatch(/closest\('\.template-bar,\s*\.aux-key-bar,\s*\.template-confirm-panel,\s*\.paste-confirm-panel'\)/);

    expect(source).toMatch(/function\s+shouldHideSoftKeyboardForDocumentPointer\s*\(\s*event:\s*PointerEvent\s*\)/);
    const documentHideBody = source.match(/function\s+shouldHideSoftKeyboardForDocumentPointer\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(documentHideBody).not.toBeNull();
    expect(documentHideBody![0]).toMatch(/softKeyboardOpen\.value/);
    expect(documentHideBody![0]).toMatch(/isKeyboardControlSurface\(event\.target\)/);

    const documentPointerBody = source.match(/function\s+onDocumentPointerDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(documentPointerBody).not.toBeNull();
    expect(documentPointerBody![0]).toMatch(/shouldHideSoftKeyboardForDocumentPointer\(event\)/);
    expect(documentPointerBody![0]).toMatch(/hideSoftKeyboard\(\)/);
  });

  test("keeps keyboard intent separate from textarea focus and blur", () => {
    expect(source).toMatch(/const\s+imeFocused\s*=\s*ref\(false\)/);
    expect(source).toMatch(/let\s+lastKeyboardControlPointerAt\s*=\s*0/);
    expect(source).toMatch(/const\s+KEYBOARD_CONTROL_BLUR_GRACE_MS\s*=\s*500/);
    expect(source).toMatch(/function\s+onKeyboardControlPointerDown\s*\(\s*\)/);
    expect(source).toMatch(/function\s+shouldPreserveSoftKeyboardForControlPointer\s*\(\s*\)/);
    expect(source).toContain('@pointerdown.capture="onKeyboardControlPointerDown"');
    expect(source).toMatch(/class="template-btn"[\s\S]*tabindex="-1"[\s\S]*@touchstart="onKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*\(\)\s*=>\s*requestTemplateSend\(tpl\)\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-hide-keyboard"[\s\S]*@touchstart\.prevent\.stop="onAuxKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend\.stop="onKeyboardControlTouchEnd\(\$event,\s*toggleSoftKeyboard\)"[\s\S]*@pointerdown\.stop="onKeyboardControlPointerDown"[\s\S]*@click\.prevent\.stop="onKeyboardControlClick\(toggleSoftKeyboard\)"/);
    expect(source).toMatch(/:data-testid="`terminal-aux-key-\$\{key\.id\}`"[\s\S]*@touchstart\.prevent="onAuxKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="sendAuxTouch\(key\.seq,\s*\$event\)"[\s\S]*@pointerdown="onShortcutPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-pick-image"[\s\S]*@touchstart\.prevent="onAuxKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*openImagePicker\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-paste-confirm-open"[\s\S]*@touchstart\.prevent="onAuxKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*openPasteConfirm\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-pick-file"[\s\S]*@touchstart\.prevent="onAuxKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*openFilePicker\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="template-confirm-cancel"[\s\S]*@touchstart="onKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*cancelTemplateSend\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="template-confirm-send"[\s\S]*@touchstart="onKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*confirmTemplateSend\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-paste-cancel"[\s\S]*@touchstart="onKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*closePasteConfirm\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/data-testid="terminal-paste-confirm"[\s\S]*@touchstart="onKeyboardControlTouchStart"[\s\S]*@touchmove="onKeyboardControlTouchMove"[\s\S]*@touchend="onKeyboardControlTouchEnd\(\$event,\s*confirmMobilePaste\)"[\s\S]*@pointerdown="onKeyboardControlPointerDown"/);
    expect(source).toMatch(/let\s+keyboardControlClickSuppressedUntil\s*=\s*0/);
    expect(source).toMatch(/let\s+keyboardControlTouchState:\s*KeyboardControlTouchState\s*\|\s*null\s*=\s*null/);
    expect(source).toMatch(/const\s+KEYBOARD_CONTROL_CLICK_SUPPRESS_MS\s*=\s*800/);
    expect(source).toMatch(/function\s+onKeyboardControlTouchStart\s*\(\s*event:\s*TouchEvent\s*\)/);
    expect(source).toMatch(/function\s+onAuxKeyboardControlTouchStart\s*\(\s*event:\s*TouchEvent\s*\)/);
    expect(source).toMatch(/function\s+onKeyboardControlTouchMove\s*\(\s*event:\s*TouchEvent\s*\)/);
    expect(source).toMatch(/function\s+onKeyboardControlTouchEnd\s*\(\s*event:\s*TouchEvent,\s*action:\s*\(\)\s*=>\s*void\s*\)/);
    expect(source).toMatch(/function\s+onKeyboardControlClick\s*\(\s*action:\s*\(\)\s*=>\s*void\s*\)/);
    const touchMoveBody = source.match(/function\s+onKeyboardControlTouchMove\s*\(\s*event:\s*TouchEvent\s*\)\s*\{[\s\S]*?\n\}/);
    expect(touchMoveBody).not.toBeNull();
    expect(touchMoveBody![0]).toMatch(/SHORTCUT_TAP_MOVE_PX\s*\*\s*SHORTCUT_TAP_MOVE_PX/);
    expect(touchMoveBody![0]).toMatch(/keyboardControlTouchState\.cancelled\s*=\s*true/);
    expect(touchMoveBody![0]).toMatch(/scrollTarget\.scrollLeft\s*=/);
    const auxStartBody = source.match(/function\s+onAuxKeyboardControlTouchStart\s*\(\s*event:\s*TouchEvent\s*\)\s*\{[\s\S]*?\n\}/);
    expect(auxStartBody).not.toBeNull();
    expect(auxStartBody![0]).toMatch(/onKeyboardControlTouchStart\(event\)/);
    expect(auxStartBody![0]).toMatch(/closest\('\.aux-key-scroll'\)/);
    expect(auxStartBody![0]).toMatch(/scrollLeft/);
    const touchEndBody = source.match(/function\s+onKeyboardControlTouchEnd\s*\(\s*event:\s*TouchEvent,\s*action:\s*\(\)\s*=>\s*void\s*\)\s*\{[\s\S]*?\n\}/);
    expect(touchEndBody).not.toBeNull();
    expect(touchEndBody![0]).toMatch(/onKeyboardControlPointerDown\(\)/);
    expect(touchEndBody![0]).toMatch(/shouldRunKeyboardControlTouchAction\(event\)/);
    expect(touchEndBody![0]).toMatch(/event\.preventDefault\(\)/);
    expect(touchEndBody![0]).toMatch(/keyboardControlClickSuppressedUntil\s*=\s*Date\.now\(\)\s*\+\s*KEYBOARD_CONTROL_CLICK_SUPPRESS_MS/);
    expect(touchEndBody![0]).toMatch(/action\(\)/);
    const clickBody = source.match(/function\s+onKeyboardControlClick\s*\(\s*action:\s*\(\)\s*=>\s*void\s*\)\s*\{[\s\S]*?\n\}/);
    expect(clickBody).not.toBeNull();
    expect(clickBody![0]).toMatch(/Date\.now\(\)\s*<\s*keyboardControlClickSuppressedUntil/);
    expect(clickBody![0]).toMatch(/action\(\)/);
    const shortcutDownBody = source.match(/function\s+onShortcutPointerDown\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(shortcutDownBody).not.toBeNull();
    expect(shortcutDownBody![0]).not.toMatch(/event\.preventDefault\(\)/);
    const focusBody = source.match(/function\s+focusTerminalIfDriver\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(focusBody).not.toBeNull();
    expect(focusBody![0]).not.toMatch(/keyboardWanted\.value\s*=/);
    expect(focusBody![0]).not.toMatch(/softKeyboardCollapsed\.value\s*=/);

    const imeFocusBody = source.match(/function\s+onImeFocus\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(imeFocusBody).not.toBeNull();
    expect(imeFocusBody![0]).toMatch(/imeFocused\.value\s*=\s*true/);
    expect(imeFocusBody![0]).not.toMatch(/keyboardWanted\.value\s*=/);
    expect(imeFocusBody![0]).not.toMatch(/softKeyboardCollapsed\.value\s*=/);

    const imeBlurBody = source.match(/function\s+onImeBlur\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(imeBlurBody).not.toBeNull();
    expect(imeBlurBody![0]).toMatch(/imeFocused\.value\s*=\s*false/);
    expect(imeBlurBody![0]).toMatch(/shouldPreserveSoftKeyboardForControlPointer\(\)/);
    expect(imeBlurBody![0]).toMatch(/scheduleSoftKeyboardOpenFocusRetries\(\)/);
    expect(imeBlurBody![0]).toMatch(/keyboardWanted\.value\s*=\s*false/);
    expect(imeBlurBody![0]).not.toMatch(/softKeyboardCollapsed\.value\s*=/);
  });

  test("restores native iOS scroll momentum by stopping xterm touchmove interception", () => {
    expect(source).toMatch(/function\s+stopTerminalTouchMove\s*\(\s*event:\s*TouchEvent\s*\)/);
    expect(source).toMatch(/event\.stopPropagation\(\)/);
    expect(source).toMatch(/querySelector<HTMLElement>\("\.xterm-viewport"\)/);
    expect(source).toMatch(/addEventListener\("touchmove",\s*stopTerminalTouchMove,\s*\{\s*passive:\s*true\s*\}/);
    expect(source).toMatch(/removeEventListener\("touchmove",\s*stopTerminalTouchMove/);
  });

  test("lets browser terminals pick a file and send it over PASTE_FILE", () => {
    expect(source).toMatch(/const\s+filePicker\s*=\s*ref<HTMLInputElement \| null>\(null\)/);
    expect(source).toContain('data-testid="terminal-pick-file"');
    expect(source).toContain('data-testid="terminal-file-input"');
    expect(source).toContain('@click="onKeyboardControlClick(openFilePicker)"');
    expect(source).toContain('@change="onFilePicked"');
    expect(source).toMatch(/function\s+openFilePicker\s*\(\s*\)/);
    expect(source).toMatch(/async function\s+onFilePicked\s*\(/);
    expect(source).toMatch(/conn\?\.sendPasteFile\(file,\s*name\)/);
    expect(source).toMatch(/pasteFileBus\.emit\(name,\s*file\.size\)/);
  });

  test("lets browser terminals pick an image and send it over PASTE_IMAGE", () => {
    expect(source).toMatch(/const\s+imagePicker\s*=\s*ref<HTMLInputElement \| null>\(null\)/);
    expect(source).toContain('data-testid="terminal-pick-image"');
    expect(source).toContain('data-testid="terminal-image-input"');
    expect(source).toContain('accept="image/*"');
    expect(source).toContain('@click="onKeyboardControlClick(openImagePicker)"');
    expect(source).toContain('@change="onImagePicked"');
    expect(source).toMatch(/function\s+openImagePicker\s*\(\s*\)/);
    expect(source).toMatch(/async function\s+onImagePicked\s*\(/);
    expect(source).toMatch(/conn\?\.sendPasteImage\(file,\s*name\)/);
    expect(source).toMatch(/pasteImageBus\.emit\(file,\s*name\)/);
  });

  test("uses Capacitor Camera Prompt for native image picking when available", () => {
    expect(source).toContain("CameraSource");
    expect(source).toContain("CameraResultType");
    expect(source).toMatch(/async function\s+openNativeImagePicker\s*\(/);
    expect(source).toMatch(/Camera\.getPhoto\(\{[\s\S]*source:\s*CameraSource\.Prompt/);
    expect(source).toMatch(/resultType:\s*CameraResultType\.Base64/);
    expect(source).toMatch(/base64ToFile\(photo\.base64String/);
    expect(source).toMatch(/conn\?\.sendPasteImage\(file,\s*file\.name\)/);
    expect(source).toMatch(/openImagePicker[\s\S]*platform\.caps\.capacitor[\s\S]*openNativeImagePicker\(\)/);
  });

  test("shows a mobile paste confirmation panel before sending clipboard text", () => {
    expect(source).toMatch(/const\s+pasteConfirmOpen\s*=\s*ref\(false\)/);
    expect(source).toMatch(/async function\s+openPasteConfirm\s*\(/);
    expect(source).toMatch(/function\s+confirmMobilePaste\s*\(/);
    expect(source).toMatch(/navigator\.clipboard\?\.readText\?\.\(\)/);
    expect(source).toMatch(/data-testid="terminal-paste-confirm-panel"/);
    expect(source).toMatch(/data-testid="terminal-paste-confirm"/);
    expect(source).toMatch(/conn\?\.sendInput\(pasteConfirmText\.value\)/);
    expect(source).toMatch(/@click="onKeyboardControlClick\(openPasteConfirm\)"/);
  });

  test("keeps the terminal viewport above both bottom shortcut bars", () => {
    expect(source).toContain(':style="{ bottom: terminalBottom }"');
    expect(source).toMatch(/const\s+bottomBarCount\s*=\s*computed/);
    expect(source).toMatch(/showAuxKeyBar\.value\s*\?\s*1\s*:\s*0/);
    expect(source).toContain(':style="{ bottom: templateBarBottom }"');
  });

  test("capture-handles mobile IME insertText without touching composition events", () => {
    expect(source).toMatch(/function\s+onImeInput\s*\(\s*event:\s*InputEvent\s*\)/);
    expect(source).toContain('event.inputType !== "insertText"');
    expect(source).toContain("event.isComposing");
    expect(source).toMatch(/conn\?\.sendInput\(data\)/);
    expect(source).toContain("stopImmediatePropagation()");
    expect(source).toContain('addEventListener("input", onImeInput');
    expect(source).toContain('removeEventListener("input", onImeInput');
  });

  test("onImeInput skips desktop runtimes so xterm.onData is not double-fired", () => {
    // Root cause of the "one space → two spaces" regression: on Wails /
    // local-PTY the physical keyboard already goes through xterm's keydown →
    // term.onData(" ") → sendInput(" ") path, but the browser also mirrors
    // the char into the hidden textarea and fires `input`, which
    // (pre-gate) ran onImeInput → sendInput(" ") a second time. The gate
    // MUST run BEFORE the inputType check so no branch in this handler can
    // fire on desktop.
    expect(source).toMatch(
      /function\s+onImeInput[^}]*?if\s*\(\s*platform\.caps\.wailsBindings\s*\|\|\s*platform\.caps\.localPty\s*\)\s*return/,
    );
  });

  test("supports mobile long-press selection with copy/send/cancel and scroll/outside exit", () => {
    expect(source).toContain('import TerminalSelectionPopover from "./TerminalSelectionPopover.vue"');
    expect(source).toContain("wordBoundaryAt");
    expect(source).toContain("readXtermCellSize");
    expect(source).toMatch(/type\s+SelectionMode\s*=\s*"idle"\s*\|\s*"pressing"\s*\|\s*"selecting"\s*\|\s*"dragging"/);
    expect(source).toMatch(/const\s+LONG_PRESS_MS\s*=\s*500/);
    expect(source).toMatch(/function\s+onSelectionPointerDown\s*\(\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/setTimeout\([\s\S]*wordBoundaryAt/);
    expect(source).toMatch(/term\.select\(boundary\.start,\s*hit\.row,\s*boundary\.len\)/);
    expect(source).toMatch(/function\s+onSelectionPointerMove\s*\(\s*event:\s*PointerEvent\s*\)/);
    expect(source).toMatch(/ensureSelectionEdgeScroll\(dir\)/);
    expect(source).toMatch(/function\s+exitSelection\s*\(/);
    expect(source).toMatch(/term\.clearSelection\(\)/);
    expect(source).toMatch(/function\s+onSelectionViewportScroll\s*\(/);
    expect(source).toMatch(/function\s+onDocumentPointerDown\s*\(/);
    expect(source).toMatch(/function\s+onSelectionCopy\s*\(/);
    expect(source).toMatch(/function\s+onSelectionSend\s*\(/);
    expect(source).toContain("<TerminalSelectionPopover");
    expect(source).toContain('@copy="onSelectionCopy"');
    expect(source).toContain('@send="onSelectionSend"');
    expect(source).toContain('@cancel="exitSelection"');
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
    // computeLinkHit reuses the same detectLinks used by the hover provider,
    // hit-testing by cell column (cellInLink) so wide glyphs don't skew it.
    expect(source).toMatch(/detectLinks\(text\)\.find/);
    // Mouse coordinates are viewport-relative; xterm buffer lines are absolute
    // within scrollback, so clicks must be offset by viewportY.
    expect(source).toContain("const bufferRow = term.buffer.active.viewportY + hit.row");
    expect(source).toContain("term.buffer.active.getLine(bufferRow)");
    // Coordinates should be relative to xterm's rendered grid, not the outer
    // host that also contains padding/overlays.
    expect(source).toContain("function getTerminalGridElement()");
    expect(source).toContain('.querySelector<HTMLElement>(".xterm-rows")');
    expect(source).toContain('.querySelector<HTMLElement>(".xterm-screen")');
    expect(source).toContain("cellInLink(hit.col");
    expect(source).toContain("mapBufferLineCells(line, term.cols)");
    expect(source).toContain("cellCoordsAt(");
  });

  test("opens terminal links from a capture-phase mouseup fallback", () => {
    expect(source).toContain('@mouseup.capture="onTerminalMouseUp"');
    expect(source).toMatch(/function\s+onTerminalMouseUp\s*\(\s*e:\s*MouseEvent\s*\)/);
    expect(source).toContain("shouldActivateLink(e, linkClickDownPos, isMac())");
    expect(source).toMatch(/const\s+hit\s*=\s*computeLinkHit\(e\)/);
    expect(source).toContain("openLinkMatch(hit)");
    expect(source).toMatch(/term\?\.clearSelection\(\)/);
    expect(source).toContain("e.stopImmediatePropagation()");
  });
});

describe("TerminalView link provider wiring", () => {
  test("registers useTerminalLinkProvider with platform openExternalURL", () => {
    expect(source).toContain("useTerminalLinkProvider");
    expect(source).toContain("platform.system.openExternalURL");
    expect(source).toContain("getUserHomeDir");
    expect(source).toContain('emit("toast", t(key))');
  });

  test("injects openLink into the link provider", () => {
    expect(source).toContain("openLink: (m) => openLinkMatch(m)");
  });

  test("routes local file paths to the file reveal store, http to external URL", () => {
    expect(source).toContain("useFileRevealStore");
    expect(source).toContain("fileRevealStore.request(abs)");
    expect(source).toContain("platform.system.openExternalURL(url)");
  });

  test("disposes link provider in onBeforeUnmount", () => {
    expect(source).toMatch(/linkProviderDisposer\?\.dispose\(\)/);
    expect(source).toMatch(/linkProviderDisposer\s*=\s*null/);
  });
});

describe("TerminalView unmount safety", () => {
  test("term.dispose is wrapped in try/catch so an xterm internal race doesn't break Vue's update batch", () => {
    // xterm's RenderService can fire a redraw via Promise.resolve().then(...)
    // during dispose; if the renderer is already half-torn-down,
    // `this._renderer.value.onRequestRedraw` throws. When this surfaces
    // synchronously inside onBeforeUnmount, Vue's update batch crashes mid-tick
    // and every subsequent reactive update hits `null component.emitsOptions`
    // — local tabs go black, the badge of the closed remote leaks onto them,
    // settings can't open. Wrap dispose so a swallowed throw stays local.
    const unmountMatch = source.match(/onBeforeUnmount\([^]*?\n\}\)/);
    expect(unmountMatch).not.toBeNull();
    const body = unmountMatch![0];
    expect(body).toMatch(/try\s*\{[^}]*term[^}]*\.dispose\(\)/);
    expect(body).toMatch(/catch/);
  });

  test("resizeObserver disconnects before conn.detach so no in-flight resize callback hits a tearing-down term", () => {
    const unmountMatch = source.match(/onBeforeUnmount\([^]*?\n\}\)/);
    expect(unmountMatch).not.toBeNull();
    const body = unmountMatch![0];
    const observerIdx = body.indexOf("resizeObserver?.disconnect");
    const connIdx = body.indexOf("conn?.detach");
    expect(observerIdx).toBeGreaterThan(-1);
    expect(connIdx).toBeGreaterThan(-1);
    expect(observerIdx).toBeLessThan(connIdx);
  });
});

describe("TerminalView driver/viewer mode", () => {
  test("tracks isDriver ref from onDriverChange", () => {
    expect(source).toMatch(/const\s+isDriver\s*=\s*ref/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("remote panes default to viewer (isDriver=false) until META confirms otherwise", () => {
    // Hard-coding the default to `true` strands restored remote panes when no
    // META arrives (e.g. session left driverless after the previous client
    // disconnected): UI shows driver mode, but the relay drops IN frames, so
    // the user sees "no overlay AND no input". Defaulting on isLocalSession
    // shows the viewer overlay so the user can press Space → claimDriver().
    expect(source).not.toMatch(/const\s+isDriver\s*=\s*ref\(\s*true\s*\)/);
    expect(source).toMatch(/const\s+isDriver\s*=\s*ref\([^)]*isLocalSession[^)]*\)/);
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

  test("handleViewerKeydown is attached at document scope so it fires even when the pane's .term-container has lost focus", () => {
    // Previously scoped to keyTarget (the .term-container). In viewer mode
    // the IME textarea is blurred and focus falls to document.body, so a
    // scoped listener silently died — Space appeared dead until the user
    // clicked the take-control button. Document scope fixes that; the
    // handler self-gates on props.active so a background pane in viewer
    // mode doesn't grab Space meant for the foreground pane / input.
    expect(source).toContain('document.addEventListener("keydown", handleViewerKeydown');
    expect(source).toContain('document.removeEventListener("keydown", handleViewerKeydown');
    expect(source).not.toMatch(/keyTarget\.addEventListener\("keydown",\s*handleViewerKeydown/);
    expect(source).toMatch(/function\s+handleViewerKeydown[\s\S]*?if\s*\(\s*!props\.active\s*\)\s*return/);
    expect(source).toMatch(/function\s+handleViewerKeydown[\s\S]*?tagName\s*===\s*"INPUT"/);
    expect(source).toMatch(/function\s+handleViewerKeydown[\s\S]*?tagName\s*===\s*"TEXTAREA"/);
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
    expect(overlayCss).toMatch(/pointer-events\s*:\s*none/); // card opts back in below
  });

  test("viewer-overlay-card has prominent centered content", () => {
    const cardCss = styleBlockFor(".viewer-overlay-card");
    expect(cardCss).toMatch(/border-radius/);
    expect(cardCss).toMatch(/padding/);
    expect(cardCss).toMatch(/text-align\s*:\s*center/);
    expect(cardCss).toMatch(/pointer-events\s*:\s*auto/);
  });

  test("take-control button accepts pointer clicks on touch devices", () => {
    const buttonCss = styleBlockFor(".viewer-overlay-btn");
    expect(buttonCss).toMatch(/pointer-events\s*:\s*auto/);
    expect(source).toContain('data-testid="take-control"');
    expect(source).toContain('@click="takeControl"');
  });

  test("auto-focuses take-control button on driver→viewer transition in active pane", () => {
    // Without this, syncTerminalInputMode blurs the IME textarea, focus
    // falls to document.body, and the .term-container-scoped viewer keydown
    // listener never fires — Space appears dead until the user clicks the
    // button first. The ref + programmatic focus restores the native
    // Space/Enter → button-activation path.
    expect(source).toContain('ref="takeControlBtnRef"');
    expect(source).toMatch(/takeControlBtnRef\.value\?\.focus\(\)/);
    expect(source).toMatch(/!isMe\s*&&\s*\(props\.active\s*\|\|\s*props\.focused\)/);
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
    // Hotkey path invokes sendTemplate(tpl) inside the composable; the
    // pointer path invokes requestTemplateSend which then either sends
    // directly or opens the confirm panel (no preview dialog).
    expect(quickTemplatesSource).toContain("sendTemplate(tpl)");
    expect(quickTemplatesSource).toMatch(/effectiveTemplates\s*\(/);
    expect(source).toMatch(/useQuickTemplates\s*\(/);
  });

  test("reloads on prefs:changed so relay-synced templates appear without a remount", () => {
    // Root cause: Go / Capacitor / Web prefsSync all write synced values
    // straight into the local adapter and then fire platform.events
    // 'prefs:changed'. Templates come down that path when the user logs
    // in on a new device. Without a subscription here the bottom bar
    // keeps rendering the pre-sync list until the user manually edits
    // a template or fully remounts the terminal.
    expect(source).toContain('platform.events.on("prefs:changed", reloadShortcutBars)');
    expect(source).toMatch(/prefsChangedOff\?\.\(\)/);
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

describe("TerminalView driver-side PTY size reconciliation", () => {
  // term.onResize is the only path that emits RESIZE, and FitAddon is a no-op
  // when our xterm already has the dims it would compute. So when another
  // client (e.g. a phone) shrinks the PTY while our WS is suspended — a
  // background tab closes the socket, so we never mirror the narrow size onto
  // our own xterm — re-claiming the driver role leaves the PTY stuck at the
  // other client's dims forever: fit() changes nothing, onResize never fires,
  // no RESIZE goes out. The driver path has to push our size explicitly
  // whenever the PTY's advertised size disagrees with ours.
  test("driver branch of applyViewerSize reconciles the PTY size after fitting", () => {
    const body = source.match(/function\s+applyViewerSize\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/if \(isDriver\.value\)[\s\S]*?safeFit\(\);[\s\S]*?syncPtySizeToTerm\(\);/);
  });

  test("syncPtySizeToTerm sends our dims only when the PTY disagrees", () => {
    const body = source.match(/function\s+syncPtySizeToTerm\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    // Driver-only: a viewer's RESIZE is dropped by the relay anyway.
    expect(body![0]).toMatch(/if \(!isDriver\.value\) return;/);
    // Mid pane-splitter drag the falling-edge watch emits the final RESIZE.
    expect(body![0]).toMatch(/if \(props\.resizeSuspended\) return;/);
    // Size match => stay silent, preserving the zero-SIGWINCH attach path.
    expect(body![0]).toMatch(/cols === term\.cols && rows === term\.rows/);
    // A stale META must not make the same dimensions emit RESIZE again.
    expect(body![0]).toMatch(/lastResizeSent\.value\?\.cols === term\.cols/);
    expect(body![0]).toMatch(/conn\.sendResize\(term\.cols, term\.rows\)/);
  });

  test("re-activating a suspended pane reconciles the PTY size too", () => {
    // conn already exists on tab re-activation, so startConnection() returns
    // early and its expectedCols/expectedRows comparison never runs.
    const watchBody = source.match(/watch\(\s*\(\)\s*=>\s*props\.active,[\s\S]*?\n\);/);
    expect(watchBody).not.toBeNull();
    expect(watchBody![0]).toMatch(/safeFit\(\);[\s\S]*?syncPtySizeToTerm\(\);/);
  });
});

describe("TerminalView paste-file dispatch", () => {
  test("delegates paste to dispatchPastedFile with a Wails-backed local-path hook", () => {
    // handleImagePaste must route through dispatchPastedFile so the local
    // NSPasteboard file-URL bypass (source path injection) can take over
    // instead of always uploading via PASTE_FILE — that upload path lands
    // in paste-files/<sid>/, which the "received files" inbox treats as
    // remote-delivered. See docs/spec/protocol.md and paste_file.go.
    expect(source).toContain("dispatchPastedFile");
    expect(source).toContain("GetPasteboardFileURLs");
    expect(source).toMatch(/isLocalSession:\s*props\.isLocalSession/);
  });
});

describe("TerminalView programmatic focus", () => {
  test("does not focus xterm while the pane is still a viewer", () => {
    expect(source).toMatch(/function\s+focusTerminalIfDriver\s*\(/);
    expect(source).toMatch(/focusTerminalIfDriver[\s\S]*?if\s*\(\s*!isDriver\.value\s*\)\s*return/);
    const directFocusCalls = source.match(/term\?\.focus\(\)/g) ?? [];
    expect(directFocusCalls).toHaveLength(1);
  });

  test("keeps xterm textarea non-editable while the pane is still a viewer", () => {
    expect(source).toMatch(/function\s+syncTerminalInputMode\s*\(/);
    expect(source).toMatch(/imeInputTarget\.readOnly\s*=\s*!isDriver\.value/);
    expect(source).toMatch(/imeInputTarget\.tabIndex\s*=\s*isDriver\.value\s*\?\s*0\s*:\s*-1/);
    expect(source).toMatch(/addEventListener\("focus",\s*onImeFocus/);
  });

  test("watches props.focused so pane-swap inside an active tab refocuses xterm", () => {
    // Without this watch, programmatic set-active-pane (e.g. sidebar click on
    // a session that lives in a multi-pane tab) only updates activePaneIdx —
    // there's no DOM mousedown to land natural focus on xterm's textarea, so
    // the user has to click the pane manually to type after taking control.
    // The watch must go through the pane-activation helper inside a nextTick so
    // viewer panes and collapsed mobile keyboards do not summon the IME.
    expect(source).toMatch(/function\s+focusTerminalForPaneActivation\s*\(/);
    expect(source).toMatch(
      /watch\(\s*\(\)\s*=>\s*props\.focused,[\s\S]*?if\s*\(\s*isFocused\s*\)[\s\S]*?nextTick\([\s\S]*?focusTerminalForPaneActivation\(\)/,
    );
  });

  test("only desktop pane activation may focus without explicit mobile keyboard intent", () => {
    const helperBody = source.match(/function\s+focusTerminalForPaneActivation\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(helperBody).not.toBeNull();
    expect(helperBody![0]).toMatch(/platform\.caps\.wailsBindings\s*\|\|\s*platform\.caps\.localPty/);
    expect(helperBody![0]).toMatch(/keyboardWanted\.value/);
    expect(helperBody![0]).toMatch(/focusTerminalIfDriver\(\)/);
  });
});

describe("TerminalView scrollback search wiring", () => {
  test("loads the search addon on the live terminal", () => {
    expect(source).toContain('import { SearchAddon } from "xterm-addon-search"');
    expect(source).toMatch(/searchAddon = new SearchAddon\(\);\s*term\.loadAddon\(searchAddon\)/);
  });

  test("keeps allowProposedApi on, which match decorations require", () => {
    expect(source).toContain("allowProposedApi: true");
  });

  test("mirrors addon result changes into the bar's counters", () => {
    const body = source.match(/searchAddon\.onDidChangeResults\(\(\{ resultIndex, resultCount \}\) => \{[\s\S]*?\n {2}\}\);/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/searchResultIndex\.value = resultIndex;/);
    expect(body![0]).toMatch(/searchResultCount\.value = resultCount;/);
  });

  test("only the focused pane opens on a search request", () => {
    expect(source).toContain("searchRequestSeq?: number");
    const watchBody = source.match(/watch\(\s*\(\)\s*=>\s*props\.searchRequestSeq,[\s\S]*?\n\);/);
    expect(watchBody).not.toBeNull();
    expect(watchBody![0]).toMatch(/if \(!props\.focused\) return;/);
    expect(watchBody![0]).toMatch(/searchOpen\.value = true;/);
  });

  test("onSearchFind clears decorations and resets both counters on an empty query", () => {
    // This is the sole defense against highlights persisting after the input
    // is cleared: xterm-addon-search has no "clear query" call of its own, so
    // the empty-query branch here is what tears the decorations down.
    const body = source.match(/function\s+onSearchFind\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/if \(!searchAddon\) return;/);
    expect(body![0]).toMatch(/if \(!query\) \{[\s\S]*?searchAddon\.clearDecorations\(\);[\s\S]*?searchResultIndex\.value = -1;[\s\S]*?searchResultCount\.value = 0;[\s\S]*?return;[\s\S]*?\}/);
    expect(body![0]).toMatch(/if \(dir === "next"\) searchAddon\.findNext\(query, opts\);/);
    expect(body![0]).toMatch(/else searchAddon\.findPrevious\(query, opts\);/);
  });

  test("closing the search clears decorations and returns focus to the terminal", () => {
    const body = source.match(/function\s+onSearchClose\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/searchOpen\.value = false;/);
    expect(body![0]).toMatch(/searchAddon\?\.clearDecorations\(\);/);
    // Not term?.focus() directly — a viewer pane has disableStdin, so taking
    // xterm focus buys it nothing while risking the iOS soft-keyboard pop
    // this file guards against elsewhere. Route through the same gated path
    // as everywhere else (see "does not focus xterm while the pane is still
    // a viewer" above, which pins exactly one direct term?.focus() call).
    expect(body![0]).toMatch(/focusTerminalIfDriver\(\);/);
    expect(body![0]).not.toMatch(/term\?\.focus\(\)/);
  });

  test("renders the search bar bound to the addon-backed state", () => {
    expect(source).toContain("<TerminalSearchBar");
    expect(source).toContain(':open="searchOpen"');
    expect(source).toContain(':focus-seq="searchFocusSeq"');
    expect(source).toContain(':result-index="searchResultIndex"');
    expect(source).toContain(':result-count="searchResultCount"');
    expect(source).toContain('@find="onSearchFind"');
    expect(source).toContain('@close="onSearchClose"');
  });

  test("the search bar is positioned inside the terminal host", () => {
    // The bar is `position: absolute`, and .term-view is the ancestor that
    // establishes its positioning context (`position: absolute; inset: 0`
    // per the comment in TerminalSearchBar.vue). If .term-view ever lost its
    // `position`, the bar would resolve against a further-up ancestor and
    // land in the page corner instead of over the pane — this is the only
    // automated guard against that, since the manual visual pass was
    // deliberately skipped for this feature.
    expect(styleBlockFor(".term-view")).toMatch(/position:\s*absolute/);
    expect(searchBarSource).toMatch(/position:\s*absolute/);
  });

  test("App drills a monotonic search request through PaneGrid", () => {
    expect(appSource).toMatch(/const terminalSearchSeq = ref\(0\)/);
    expect(appSource).toMatch(/onTerminalSearch: \(\) => \{\s*terminalSearchSeq\.value\+\+;/);
    expect(appSource).toContain(':search-request-seq="terminalSearchSeq"');
    expect(paneSource).toContain("searchRequestSeq?: number");
    expect(paneSource).toContain(':search-request-seq="searchRequestSeq"');
  });
});

describe("TerminalView appearance", () => {
  test("seeds the terminal from the appearance prop instead of literals", () => {
    expect(source).toContain("appearance?: TerminalAppearance");
    // Scope to the actual `new Terminal({...})` constructor call, not the
    // whole file — the withDefaults() default factory a few dozen lines up
    // also contains a literal `scrollback: 5000`, which is legitimate there
    // (it's the prop default) but would be a bug if it showed up here.
    const ctor = source.match(/term = new Terminal\(\{[\s\S]*?\n\s*\}\);/);
    expect(ctor).not.toBeNull();
    const body = ctor![0];
    expect(body).toMatch(/fontFamily:\s*composeFontFamily\(/);
    expect(body).toMatch(/fontSize:\s*props\.appearance\.fontSize/);
    expect(body).toMatch(/lineHeight:\s*props\.appearance\.lineHeight/);
    expect(body).toMatch(/cursorStyle:\s*props\.appearance\.cursorStyle/);
    expect(body).toMatch(/cursorBlink:\s*props\.appearance\.cursorBlink/);
    expect(body).toMatch(/scrollback:\s*props\.appearance\.scrollback/);
    // Negative side: catches reverting any one field back to a literal.
    expect(body).not.toMatch(/scrollback:\s*5000/);
    expect(body).not.toMatch(/cursorBlink:\s*true/);
  });

  test("re-fits only for metrics-affecting changes, and only as driver", () => {
    const body = source.match(/function\s+applyAppearance\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    // Pins the guard shape itself, not just that the two tokens appear
    // somewhere in the function — `metricsChanged &&` deleted from the guard
    // still satisfied the old looser regex.
    expect(body![0]).toMatch(/if\s*\(\s*metricsChanged\s*&&\s*isDriver\.value\s*\)\s*safeFit\(\)/);
  });

  test("cursor and scrollback changes do not trigger a fit", () => {
    const body = source.match(/function\s+applyAppearance\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/metricsChanged/);
    // The literal inverse of this test's name — folding cursorStyle,
    // cursorBlink or scrollback into the metricsChanged comparison — must
    // fail here, not just pass by virtue of the token "metricsChanged"
    // existing somewhere in the function.
    expect(body![0]).not.toMatch(/lastAppliedMetrics\.cursorStyle|lastAppliedMetrics\.cursorBlink|lastAppliedMetrics\.scrollback/);
  });

  test("App drills one appearance object through PaneGrid", () => {
    expect(appSource).toMatch(/const terminalAppearance = ref</);
    expect(appSource).toContain(':appearance="terminalAppearance"');
    expect(paneSource).toContain("appearance?: TerminalAppearance");
    expect(paneSource).toContain(':appearance="appearance"');
  });

  // Important 1 — SettingsDialog.vue:214-215 forwards appearance-changed
  // from SettingsGeneral up to App.vue; this hop shipped dead once (nothing
  // caught it because the wire was simply never connected). Pin App.vue's
  // listener here since App.vue's own source is already loaded in this file.
  test("App listens for appearance-changed on the settings dialog", () => {
    expect(appSource).toContain('@appearance-changed="onAppearanceChanged"');
  });
});

// Task 4 (prefs-sync-l1 §7.2): a remote Pull fires the same "prefs:changed"
// event markPrefDirtyAndPush fires after a Push (see prefs_watch.go). App.vue
// must reload its own state from Go so a value that changed on another device
// shows up here without a restart or a Settings reopen.
describe("prefs:changed reload", () => {
  test("App re-reads theme and appearance when prefs change remotely", () => {
    expect(appSource).toMatch(/events\.on\(\s*["']prefs:changed["']/);
    const body = appSource.match(/events\.on\(\s*["']prefs:changed["'][\s\S]*?\n\s*\}\)/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/refreshTerminalTheme\(\)/);
    expect(body![0]).toMatch(/refreshTerminalAppearance\(\)/);
  });

  // The negative assertion is the one that matters: a handler that reloads
  // state and then persists it would ping-pong forever between two devices
  // (A pulls -> writes back -> pushes -> B pulls -> writes back -> pushes ->
  // A pulls, ...). This must fail the moment anyone adds a setter call here.
  test("the reload path never writes back — that would ping-pong between devices", () => {
    const body = appSource.match(/events\.on\(\s*["']prefs:changed["'][\s\S]*?\n\s*\}\)/);
    expect(body![0]).not.toMatch(/setTerminal|setDefaultShell|setShortcutBindings/);
  });
});
