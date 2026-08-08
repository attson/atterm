import { ref, onMounted, onBeforeUnmount } from 'vue'
import { getUplinkHealth, type ConnHealthSnapshot } from '../lib/api'
import { errText, logDebug } from "../lib/log";

const CLOSED_SNAPSHOT: ConnHealthSnapshot = {
  state: 'closed',
  rtt: { last_ms: null, p50_ms: null, p95_ms: null },
  rtt_samples: [],
  reconnect: { count_last_hour: 0, last_at_ms: null, last_reason: '', history: [] },
  bytes: { in_per_sec: 0, out_per_sec: 0 },
  seq_gaps: 0,
}

// useUplinkHealth polls GetUplinkHealth on a 5 s cadence while idle, 1 s while
// the drawer is open. The poll is cheap (a single Wails RPC), but the snapshot
// itself can be ~60 entries × small JSON, so 5 s feels right for the pill.
export function useUplinkHealth(opts: { fast?: () => boolean } = {}) {
  const health = ref<ConnHealthSnapshot>(CLOSED_SNAPSHOT)
  let timer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false

  async function tick() {
    try {
      health.value = await getUplinkHealth()
    } catch (e) {
      logDebug('uplink', 'health poll failed; keeping last value', { error: errText(e) })
    }
  }

  function schedule() {
    if (timer !== null) clearTimeout(timer)
    if (cancelled) return
    const delay = opts.fast?.() ? 1000 : 5000
    timer = setTimeout(async () => {
      await tick()
      schedule()
    }, delay)
  }

  onMounted(() => {
    tick()
    schedule()
  })

  onBeforeUnmount(() => {
    cancelled = true
    if (timer !== null) clearTimeout(timer)
  })

  return health
}
