import { describe, expect, it } from 'vitest'

import { createMockPlatform } from './mockPlatform'

describe('mock platform shared app bridges', () => {
  it('provides direct-connection preferences and ordered snippet persistence', async () => {
    const platform = createMockPlatform()

    expect(await platform.directConnection.load()).toBe(false)
    await platform.directConnection.save(true)
    expect(await platform.directConnection.load()).toBe(true)

    const snippets = [
      { id: 'deploy', label: 'Deploy', text: 'make build\nmake deploy' },
      { id: 'logs', label: 'Logs', text: 'journalctl -f' },
    ]
    await platform.snippets.save(snippets)
    expect(await platform.snippets.load()).toEqual(snippets)
  })
})
