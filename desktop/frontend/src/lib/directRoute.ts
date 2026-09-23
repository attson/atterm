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
  private lastCommittedSeq: number

  constructor(committedSeq = 0) {
    this.lastCommittedSeq = committedSeq
  }

  get state(): DirectRouteState { return this.routeState }
  get generation(): number { return this.routeGeneration }
  get committedSeq(): number { return this.lastCommittedSeq }

  beginDirect(): number {
    if (this.routeState !== DirectRouteState.RelayAttached) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.DirectConnecting
    return this.routeGeneration
  }

  beginDirectReplay(generation: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectConnecting) throw new Error('invalid direct route transition')
    this.routeState = DirectRouteState.DirectReplay
  }

  activateDirect(generation: number, replayedSeq: number): void {
    this.requireGeneration(generation)
    if (this.routeState !== DirectRouteState.DirectReplay || replayedSeq !== this.lastCommittedSeq) {
      throw new Error('invalid direct route transition')
    }
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
    this.routeState = DirectRouteState.RelayReattaching
    return this.routeGeneration
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
    if (generation !== this.routeGeneration || seq <= 0 || seq <= this.lastCommittedSeq) return false
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
    this.lastCommittedSeq = seq
    return true
  }

  private requireGeneration(generation: number): void {
    if (generation !== this.routeGeneration) throw new Error('stale direct route generation')
  }
}
