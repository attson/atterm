import {
  CODEX_SESSION_ID,
  CLAUDE_SESSION_ID,
  BUILD_SESSION_ID,
  GOTEST_SESSION_ID,
  IDLE_SESSION_ID,
} from './fakeSessions'

// 每个会话 attach 后终端播放的初始输出(打字机分块)。idle 会话给一个空
// prompt,等待用户交互。字符串含 ANSI,终端会渲染颜色。
export const replayScripts: Record<string, string[]> = {
  [CODEX_SESSION_ID]: [
    '\x1b[1m$ codex exec "refactor auth"\x1b[0m\r\n',
    '\x1b[36m▸ codex\x1b[0m analysing repository…\r\n',
    'found 12 files referencing auth\r\n',
    'proposing changes to internal/auth/*.go …\r\n',
    '\x1b[33m⠋ working…\x1b[0m\r\n',
  ],
  [CLAUDE_SESSION_ID]: [
    '\x1b[1m$ claude\x1b[0m\r\n',
    'I found the flaky test in session_test.go.\r\n',
    'Should I (1) add a retry or (2) fix the race?\r\n',
    '\x1b[33m❯ waiting for your input…\x1b[0m ',
  ],
  [BUILD_SESSION_ID]: [
    '\x1b[1m$ npm run build\x1b[0m\r\n',
    'vite v5.4.11 building for production…\r\n',
    '✓ 342 modules transformed.\r\n',
    '\x1b[32m✓ built in 12.0s\x1b[0m\r\n',
    '$ ',
  ],
  [GOTEST_SESSION_ID]: [
    '\x1b[1m$ go test ./...\x1b[0m\r\n',
    'ok   atterm/internal/relay   0.42s\r\n',
    '\x1b[31m--- FAIL: TestSessionReplay (0.10s)\x1b[0m\r\n',
    '\x1b[31mFAIL\x1b[0m atterm/internal/session\r\n',
    '$ ',
  ],
  [IDLE_SESSION_ID]: [
    'Last login: Fri Jul 31 10:00 on ttys001\r\n',
    'type \x1b[36mhelp\x1b[0m to see demo commands.\r\n',
    '\x1b[32myou@dev-server\x1b[0m ~/srv/atterm $ ',
  ],
}

export const PROMPT = '\x1b[32myou@dev-server\x1b[0m ~/srv/atterm $ '
