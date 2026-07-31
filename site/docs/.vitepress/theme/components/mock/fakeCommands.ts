// 假命令响应表:供 idle 会话的 mock 终端解析用户输入。output 用 \r\n 换行
// 以匹配 PTY 语义。longRunning 命令返回分步输出 + 最终任务状态,由 mockSocket
// 播放并触发通知。
export interface FakeCommandResult {
  output: string
  longRunning?: boolean
  steps?: string[] // 逐步输出(打字机),仅 longRunning
  finalState?: 'completed' | 'failed'
}

const LS = 'README.md  main.go  package.json  logo.png  src/'

export function runFakeCommand(raw: string): FakeCommandResult {
  const line = raw.trim()
  if (!line) return { output: '' }
  const [cmd, ...rest] = line.split(/\s+/)
  const arg = rest.join(' ')

  switch (cmd) {
    case 'pwd':
      return { output: '~/srv/atterm\r\n' }
    case 'whoami':
      return { output: 'you\r\n' }
    case 'ls':
      return { output: LS + '\r\n' }
    case 'echo':
      return { output: stripQuotes(arg) + '\r\n' }
    case 'cat':
      return { output: catFile(arg) }
    case 'date':
      return { output: 'Fri Jul 31 10:00:00 UTC 2026\r\n' }
    case 'help':
      return { output: helpText() }
    case 'clear':
      return { output: '\x1b[2J\x1b[H' }
    case 'codex':
    case 'claude':
    case 'aider':
      return {
        output: '',
        longRunning: true,
        finalState: 'completed',
        steps: [
          `\x1b[36m▸ ${cmd}\x1b[0m starting…\r\n`,
          'reading project files…\r\n',
          'planning changes…\r\n',
          'applying patch to auth.go…\r\n',
          '\x1b[32m✓ done\x1b[0m (3 files changed)\r\n',
        ],
      }
    default:
      return { output: `zsh: command not found: ${cmd}\r\n` }
  }
}

function stripQuotes(s: string): string {
  return s.replace(/^["']|["']$/g, '')
}

function catFile(name: string): string {
  if (name === 'README.md') return '# atterm-demo\r\n\r\nA fake project for the site demo.\r\n'
  if (name === 'package.json') return '{\r\n  "name": "atterm-demo"\r\n}\r\n'
  return `cat: ${name}: No such file or directory\r\n`
}

function helpText(): string {
  return [
    'demo shell — try:',
    '  ls  pwd  whoami  echo <text>  cat README.md  date  clear',
    '  codex/claude/aider <task>   (演示任务状态 + 通知)',
    '',
  ].join('\r\n')
}
