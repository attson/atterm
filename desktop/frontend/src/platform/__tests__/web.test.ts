import { describe, it, expect } from 'vitest'
import { createWebPlatform } from '../web'

describe('web platform', () => {
  it('caps: localPty=false autoUpdate=false pluginHost=false windowControls=false', () => {
    const p = createWebPlatform()
    expect(p.caps.localPty).toBe(false)
    expect(p.caps.autoUpdate).toBe(false)
    expect(p.caps.pluginHost).toBe(false)
    expect(p.caps.windowControls).toBe(false)
    expect(p.caps.systemClipboard).toBe(true)
    expect(p.caps.fileDialog).toBe(true)
  })
})
