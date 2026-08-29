import { registerPlugin } from '@capacitor/core'

export interface NativeServicePreviewStartOptions {
  serviceId: string
  clientTicket: string
  clientToHostKey: string
  hostToClientKey: string
  relayUrl: string
  token: string
  allowInsecure: boolean
}

interface NativeServicePreviewPlugin {
  start(options: NativeServicePreviewStartOptions): Promise<{ id: string; url: string }>
  stop(options: { id: string }): Promise<void>
}

export const NativeServicePreview = registerPlugin<NativeServicePreviewPlugin>('AttermServicePreview')
