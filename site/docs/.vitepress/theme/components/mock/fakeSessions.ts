import type { RemoteSession } from '@/platform/types'

// 固定 uuid,mock 各处用它们做 sid → 脚本/命令表映射。
export const CODEX_SESSION_ID = '11111111-1111-4111-8111-111111111111'
export const CLAUDE_SESSION_ID = '22222222-2222-4222-8222-222222222222'
export const BUILD_SESSION_ID = '33333333-3333-4333-8333-333333333333'
export const GOTEST_SESSION_ID = '44444444-4444-4444-8444-444444444444'
export const IDLE_SESSION_ID = '55555555-5555-4555-8555-555555555555'

const HOST_MBP = 'macbook-pro'
const HOST_DEV = 'dev-server'
const now = 1_753_900_000 // 固定时间戳,避免测试非确定性

export const fakeSessions: RemoteSession[] = [
  {
    session_id: CODEX_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'codex refactor auth', cwd: '~/projects/atterm', cols: 120, rows: 32,
    started_at: now - 300, task_state: 'running', current_command: 'codex exec "refactor auth"',
    command_started_at: now - 60, last_output_at: now - 1, type: 'codex', remote_permission: 'full',
  },
  {
    session_id: CLAUDE_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'claude fix flaky test', cwd: '~/projects/atterm', cols: 120, rows: 32,
    started_at: now - 600, task_state: 'waiting_input', current_command: 'claude',
    command_started_at: now - 120, attention_at: now - 30, unread: true, type: 'claude', remote_permission: 'full',
  },
  {
    session_id: BUILD_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'npm run build', cwd: '~/projects/atterm/web', cols: 100, rows: 28,
    started_at: now - 900, task_state: 'completed', current_command: 'npm run build',
    command_started_at: now - 200, command_ended_at: now - 188, command_duration_ms: 12000,
    command_exit_code: 0, type: 'shell', remote_permission: 'full',
  },
  {
    session_id: GOTEST_SESSION_ID, host_id: HOST_DEV, host: HOST_DEV, user: 'you',
    title: 'go test ./...', cwd: '~/srv/atterm', cols: 110, rows: 30,
    started_at: now - 1200, task_state: 'failed', current_command: 'go test ./...',
    command_started_at: now - 400, command_ended_at: now - 380, command_duration_ms: 20000,
    command_exit_code: 1, type: 'shell', remote_permission: 'full',
  },
  {
    session_id: IDLE_SESSION_ID, host_id: HOST_DEV, host: HOST_DEV, user: 'you',
    title: 'zsh', cwd: '~/srv/atterm', cols: 110, rows: 30,
    started_at: now - 1500, task_state: 'idle', type: 'shell', remote_permission: 'full',
  },
]
