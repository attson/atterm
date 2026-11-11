export enum DirectRoute {
  None = 0,
  Relay = 1,
  Direct = 2,
}

export enum DirectRouteState {
  RelayAttached = 1,
  DirectConnecting = 2,
  DirectReplay = 3,
  DirectActive = 4,
  RelayReattaching = 5,
}

/** Holds the route generation, output cursor, and sole input writer for one
 * session_id. Transport callbacks must carry the generation they were opened
 * under so late events cannot affect a replacement route. */
export class DirectRouteTracker {
  private routeState = DirectRouteState.RelayAttached
  private routeGeneration = 1
  private relayGeneration = 1
  private lastCommittedSeq: number
  private directStartSeq = 0
  private directObservedSeq = 0
  private directReady = false

  constructor(committedSeq = 0) {
    this.lastCommittedSeq = committedSeq
  }

  get state(): DirectRouteState { return this.routeState }
  get generation(): number { return this.routeGeneration }
  get committedSeq(): number { return this.lastCommittedSeq }

  beginDirect(): number {
    if (this.routeState !== DirectRouteState.RelayAttached) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.DirectConnecting
    this.directStartSeq = this.lastCommittedSeq
    this.directObservedSeq = this.lastCommittedSeq
    this.directReady = false
    return this.routeGeneration
  }

  beginDirectReplay(generation: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectConnecting) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.DirectReplay
  }

  noteDirectReady(generation: number, replayedSeq: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectReplay || !Number.isSafeInteger(replayedSeq) || replayedSeq < this.directStartSeq) {
      throw new Error('invalid direct route transition')
    }
    this.directObservedSeq = Math.max(this.directObservedSeq, replayedSeq)
    this.directReady = true
  }

  canActivateDirect(generation: number): boolean {
    return generation === this.routeGeneration &&
      this.routeState === DirectRouteState.DirectReplay &&
      this.directReady &&
      this.directObservedSeq >= this.lastCommittedSeq
  }

  activateDirect(generation: number): void {
    this.requireGeneration(generation)
    if (!this.canActivateDirect(generation)) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.DirectActive
  }

  abortDirect(generation: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectConnecting && this.routeState !== DirectRouteState.DirectReplay) {
      throw new Error('invalid direct route transition')
    }
    this.routeGeneration++
    this.routeState = DirectRouteState.RelayAttached
  }

  directLost(generation: number): number {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectActive) throw new Error('invalid direct route transition')
    this.routeGeneration++
    this.relayGeneration = this.routeGeneration
    this.routeState = DirectRouteState.RelayReattaching
    return this.routeGeneration
  }

  /** Marks a newly opened Relay socket as the live Relay generation. An
   * aborted direct probe advances routeGeneration while the existing Relay
   * remains valid, so Relay and direct generations cannot be one field. */
  useRelayGeneration(generation: number): boolean {
    if (generation !== this.routeGeneration || this.routeState === DirectRouteState.DirectActive) return false
    this.relayGeneration = generation
    return true
  }

  relayAttached(generation: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.RelayReattaching) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.RelayAttached
  }

  inputRoute(): DirectRoute {
    switch (this.routeState) {
      case DirectRouteState.RelayAttached:
      case DirectRouteState.DirectConnecting:
      case DirectRouteState.DirectReplay:
        return DirectRoute.Relay
      case DirectRouteState.DirectActive:
        return DirectRoute.Direct
      default:
        return DirectRoute.None
    }
  }

  acceptOutput(generation: number, route: DirectRoute, seq: number): boolean {
    if (!this.acceptsGeneration(generation, route) || seq <= 0) return false
    let allowed = false
    switch (this.routeState) {
      case DirectRouteState.RelayAttached:
      case DirectRouteState.DirectConnecting:
      case DirectRouteState.RelayReattaching:
        allowed = route === DirectRoute.Relay
        break
      case DirectRouteState.DirectReplay:
        allowed = route === DirectRoute.Relay || route === DirectRoute.Direct
        break
      case DirectRouteState.DirectActive:
        allowed = route === DirectRoute.Direct
        break
    }
    if (!allowed) return false
    if (route === DirectRoute.Direct && this.routeState === DirectRouteState.DirectReplay) {
      this.directObservedSeq = Math.max(this.directObservedSeq, seq)
    }
    if (seq <= this.lastCommittedSeq) return false
    this.lastCommittedSeq = seq
    return true
  }

  acceptsRoute(generation: number, route: DirectRoute): boolean {
    if (!this.acceptsGeneration(generation, route)) return false
    switch (this.routeState) {
      case DirectRouteState.RelayAttached:
      case DirectRouteState.DirectConnecting:
      case DirectRouteState.RelayReattaching:
        return route === DirectRoute.Relay
      case DirectRouteState.DirectReplay:
        return route === DirectRoute.Relay || route === DirectRoute.Direct
      case DirectRouteState.DirectActive:
        return route === DirectRoute.Direct
    }
    return false
  }

  private acceptsGeneration(generation: number, route: DirectRoute): boolean {
    if (route === DirectRoute.Relay) return generation === this.relayGeneration
    if (route === DirectRoute.Direct) return generation === this.routeGeneration
    return false
  }

  private requireGeneration(generation: number): void {
    if (generation !== this.routeGeneration) throw new Error('stale direct route generation')
  }
}
