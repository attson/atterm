// Module declarations for cross-package @shared/* imports.
//
// At build time, vite resolves @shared/* to ../../web/src/shared/* via
// the alias in vite.config.ts. At type-check time, vue-tsc cannot follow
// those imports without pulling the web/ project into its include tree
// (which causes `vue` resolution to fail because `vue` lives in web's
// node_modules, not desktop's). These declarations give vue-tsc just
// enough shape information for the desktop frontend's usage; type
// correctness of the components themselves is enforced by the web/
// project's own vue-tsc run.

declare module '@shared/components/ConnHealthPill.vue' {
  import type { DefineComponent } from 'vue'
  import type { ConnHealthSnapshot } from '@shared/connhealth/connhealth'
  const Component: DefineComponent<{
    health: ConnHealthSnapshot
    labels?: {
      connecting?: string
      reconnecting?: string
      off?: string
      unknownMS?: string
    }
  }>
  export default Component
}

declare module '@shared/components/ConnHealthDrawer.vue' {
  import type { DefineComponent } from 'vue'
  import type { ConnHealthSnapshot } from '@shared/connhealth/connhealth'
  const Component: DefineComponent<{
    health: ConnHealthSnapshot
    open: boolean
    labels?: {
      title?: string
      rttNow?: string
      rttP50P95?: string
      bytesIn?: string
      bytesOut?: string
      state?: string
      reconnectsLastHour?: string
      reconnectsTime?: string
      reconnectsReason?: string
      reconnectsDowntime?: string
      seqGaps?: string
      close?: string
    }
  }>
  export default Component
}

declare module '@shared/connhealth/connhealth' {
  export type ConnState = 'closed' | 'connecting' | 'connected' | 'reconnecting'
  export interface RTTSample {
    at_ms: number
    rtt_ms: number
  }
  export interface ReconnectEvent {
    at_ms: number
    reason: string
    duration_ms: number
  }
  export interface ConnHealthSnapshot {
    state: ConnState
    rtt: { last_ms: number | null; p50_ms: number | null; p95_ms: number | null }
    rtt_samples: RTTSample[]
    reconnect: {
      count_last_hour: number
      last_at_ms: number | null
      last_reason: string
      history: ReconnectEvent[]
    }
    bytes: { in_per_sec: number; out_per_sec: number }
    seq_gaps: number
  }
}
