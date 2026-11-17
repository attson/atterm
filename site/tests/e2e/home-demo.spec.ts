import { expect, test, type Page } from '@playwright/test'

const RETRY_QUERY = 'atterm-demo-retry'

function captureRuntimeFailures(page: Page) {
  const failures: string[] = []
  page.on('pageerror', (error) => failures.push(error.message))
  page.on('console', (message) => {
    const text = message.text()
    if (message.type() === 'error' || text.includes('GetUplinkHealth is not a function')) {
      failures.push(text)
    }
  })
  return failures
}

async function pasteDemoImage(page: Page) {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.evaluate(() => navigator.clipboard.writeText(
    '/home/attson/Pictures/素材/demo-shot.jpeg',
  ))
  await page.keyboard.press('Control+V')
}

test('defers the real app behind one clear start action', async ({ page }) => {
  const appRequests: string[] = []
  page.on('request', (request) => {
    if (/\/chunks\/App\.[^/]+\.js$/.test(request.url())) appRequests.push(request.url())
  })
  const failures = captureRuntimeFailures(page)

  await page.goto('./')
  await expect(page.getByTestId('demo-poster')).toBeVisible()
  await expect(page.getByTestId('demo-start')).toHaveCount(1)
  await expect(page.getByTestId('demo-start')).toHaveText('开始体验')
  await expect(page.getByText('AI 等待输入')).toHaveCount(0)
  await expect(page.getByText('文件浏览', { exact: true })).toHaveCount(0)
  await page.waitForTimeout(500)
  expect(appRequests).toEqual([])

  await page.getByTestId('demo-start').click()
  await expect(page.locator('.home-demo-frame .app')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('type help', { timeout: 10_000 })
  expect(appRequests.length).toBeGreaterThan(0)
  expect(failures).toEqual([])
})

test('supports input history, snippet hosts, and keeps connection health inside the demo', async ({ page }) => {
  const failures = captureRuntimeFailures(page)
  await page.goto('./')
  await page.getByTestId('demo-start').click()
  await expect(page.locator('.home-demo-frame .app')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('type help', { timeout: 10_000 })

  await page.locator('.home-demo-frame').getByRole('button', { name: 'yes', exact: true }).click()
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('command not found: yes')

  const terminalInput = page.getByRole('textbox', { name: 'Terminal input' })
  await terminalInput.click()
  await page.keyboard.type('codex')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('Codex CLI · interactive demo')
  await page.keyboard.type('review auth')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('Demo response ready')
  await page.keyboard.type('/exit')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('Leaving Codex CLI demo')

  await page.keyboard.type('ac')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.type('b')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Backspace')
  await page.keyboard.type('c')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('command not found: abc')

  await page.locator('[data-test="session-input-history-trigger"]').click()
  await expect(page.locator('[data-test="session-input-history-panel"]')).toContainText('yes')
  await expect(page.locator('[data-test="session-input-history-error"]')).toHaveCount(0)
  const historyHeaderHeight = await page.locator('.session-input-history-header').evaluate((header) => header.getBoundingClientRect().height)
  expect(historyHeaderHeight).toBeLessThanOrEqual(40)
  await page.locator('[data-test="session-input-history-close"]').click()

  await page.getByTestId('snippet-trigger').hover()
  await expect(page.getByTestId('snippet-panel')).toBeVisible()
  await page.getByTestId('snippet-batch-ports').click({ force: true })
  const frameBeforeModal = await page.locator('.home-demo-frame').boundingBox()
  const snippetOverlay = await page.locator('.snippet-run-overlay').boundingBox()
  expect(frameBeforeModal).not.toBeNull()
  expect(snippetOverlay).not.toBeNull()
  expect(snippetOverlay!.x).toBeGreaterThanOrEqual(frameBeforeModal!.x)
  expect(snippetOverlay!.x + snippetOverlay!.width).toBeLessThanOrEqual(frameBeforeModal!.x + frameBeforeModal!.width)
  expect(snippetOverlay!.y).toBeGreaterThanOrEqual(frameBeforeModal!.y)
  expect(snippetOverlay!.y + snippetOverlay!.height).toBeLessThanOrEqual(frameBeforeModal!.y + frameBeforeModal!.height)
  await page.getByTestId('snippet-target-hosts').click()
  await expect(page.getByText('staging.example.com')).toBeVisible()
  await expect(page.getByTestId('snippet-run-error')).toHaveCount(0)
  await page.getByTestId('snippet-run-host-demo-staging').check()
  await page.getByTestId('snippet-run-start').click()
  await expect(page.getByText('Demo command completed successfully.')).toBeVisible()
  await page.getByTestId('snippet-run-close').click()

  await page.getByRole('button', { name: '切换应用主题' }).click()
  await expect(page.locator('.home-demo-toast', { hasText: 'SetTerminalTheme' })).toHaveCount(0)

  await page.getByTestId('titlebar-conn-health-pill').click()
  const frame = await page.locator('.home-demo-frame').boundingBox()
  const drawer = await page.getByRole('dialog', { name: '连接质量' }).boundingBox()
  expect(frame).not.toBeNull()
  expect(drawer).not.toBeNull()
  expect(drawer!.x).toBeGreaterThanOrEqual(frame!.x)
  expect(drawer!.x + drawer!.width).toBeLessThanOrEqual(frame!.x + frame!.width)
  expect(drawer!.y).toBeGreaterThanOrEqual(frame!.y)
  expect(drawer!.y + drawer!.height).toBeLessThanOrEqual(frame!.y + frame!.height)
  expect(failures).toEqual([])
})

test('pastes an image into an interactive AI session and keeps accepting instructions', async ({ page }) => {
  const failures = captureRuntimeFailures(page)
  await page.goto('./')
  await page.getByTestId('demo-start').click()
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('type help', { timeout: 10_000 })

  const terminalInput = page.getByRole('textbox', { name: 'Terminal input' })
  await terminalInput.click()
  await page.keyboard.type('codex')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('Codex CLI · interactive demo')

  await pasteDemoImage(page)
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('[Image #1]')
  await expect(page.locator('.home-demo-frame .xterm-rows')).not.toContainText('received the image')
  await expect(page.locator('.home-demo-frame .xterm-rows')).not.toContainText('demo-shot.jpeg')
  await expect(page.locator('.home-demo-frame .xterm-rows')).not.toContainText('/home/attson/Pictures')
  const pasteToast = page.getByTestId('paste-toast')
  await expect(pasteToast).toBeVisible()
  await expect(pasteToast).toContainText('demo-shot.jpeg')
  const [frameBox, toastBox] = await Promise.all([
    page.locator('.home-demo-frame').boundingBox(),
    pasteToast.boundingBox(),
  ])
  expect(frameBox).not.toBeNull()
  expect(toastBox).not.toBeNull()
  expect(Math.abs(frameBox!.x + frameBox!.width - toastBox!.x - toastBox!.width - 12)).toBeLessThanOrEqual(2)
  expect(Math.abs(toastBox!.y - frameBox!.y - 88)).toBeLessThanOrEqual(2)

  const imageRow = page.locator('.home-demo-frame .xterm-rows > div')
    .filter({ hasText: '[Image #1]' })
    .last()
  const rowBox = await imageRow.boundingBox()
  expect(rowBox).not.toBeNull()
  await page.mouse.move(rowBox!.x + 52, rowBox!.y + rowBox!.height / 2)

  const hoverPreview = page.locator('.home-demo-frame .atterm-image-hover')
  await expect(hoverPreview).toBeVisible()
  await expect.poll(() => hoverPreview.locator('img').evaluate((image) => ({
    naturalWidth: (image as HTMLImageElement).naturalWidth,
    naturalHeight: (image as HTMLImageElement).naturalHeight,
  }))).toEqual({ naturalWidth: 240, naturalHeight: 160 })
  const previewBox = await hoverPreview.boundingBox()
  expect(previewBox).not.toBeNull()
  expect(previewBox!.width).toBeGreaterThanOrEqual(240)
  expect(previewBox!.height).toBeGreaterThanOrEqual(160)
  expect(Math.abs(previewBox!.x - rowBox!.x - 64)).toBeLessThanOrEqual(2)
  expect(previewBox!.y).toBeGreaterThan(rowBox!.y)
  await page.mouse.move(rowBox!.x + 220, rowBox!.y + rowBox!.height / 2)
  await expect(hoverPreview).toHaveCount(0)
  await expect(pasteToast).toBeHidden({ timeout: 6_000 })

  await terminalInput.click()
  await page.keyboard.type('describe this image')
  await expect(page.locator('.home-demo-frame .xterm-rows > div').filter({ hasText: '[Image #1]' }))
    .toContainText('[Image #1] describe this image')
  await page.keyboard.press('Enter')
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('is working on: describe this image')
  await page.locator('[data-test="session-input-history-trigger"]').click()
  await expect(page.locator('[data-test="session-input-history-panel"]'))
    .toContainText('[Image #1] describe this image')
  await page.locator('[data-test="session-input-history-close"]').click()
  expect(failures).toEqual([])
})

test('shows a load error and retries the demo from a clean page', async ({ page }) => {
  const appChunk = /\/chunks\/App\.[^/]+\.js$/
  await page.route(appChunk, (route) => route.abort('failed'))
  await page.goto('./')

  await page.getByTestId('demo-start').click()
  await expect(page.getByRole('alert')).toContainText('Demo 加载失败', { timeout: 15_000 })
  await expect(page.getByTestId('demo-retry')).toBeVisible()

  await page.unroute(appChunk)
  await page.getByTestId('demo-retry').click()
  await expect(page.locator('.home-demo-frame .app')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.home-demo-frame .xterm-rows')).toContainText('type help', { timeout: 10_000 })
  await expect(page).not.toHaveURL(new RegExp(RETRY_QUERY))
})

test('restores page globals and preserves user storage after the demo unmounts', async ({ page }) => {
  await page.goto('./')
  await page.evaluate(() => {
    const state = window as typeof window & Record<string, any>
    state.__demoNativeWebSocket = window.WebSocket
    state.__demoPreviousGo = { sentinel: 'keep' }
    state.go = state.__demoPreviousGo
    localStorage.setItem('site-user-value', 'keep')
  })

  await page.getByTestId('demo-start').click()
  await expect(page.locator('.home-demo-frame .app')).toBeVisible({ timeout: 15_000 })
  await page.getByRole('link', { name: '使用文档' }).first().click()
  await expect(page).toHaveURL(/\/atterm\/guide\/$/)

  const restored = await page.evaluate(() => {
    const state = window as typeof window & Record<string, any>
    return {
      websocket: window.WebSocket === state.__demoNativeWebSocket,
      go: state.go === state.__demoPreviousGo,
      storage: localStorage.getItem('site-user-value'),
      lang: document.documentElement.lang,
    }
  })
  expect(restored).toEqual({ websocket: true, go: true, storage: 'keep', lang: 'zh-CN' })
})

test.describe('mobile home', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('keeps the page and the running demo within the viewport', async ({ page }) => {
    const failures = captureRuntimeFailures(page)
    await page.goto('./')
    await expect(page.getByTestId('demo-poster')).toBeVisible()

    const initialOverflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(initialOverflow).toBeLessThanOrEqual(1)

    await page.getByTestId('demo-start').click()
    await expect(page.locator('.home-demo-frame .app')).toBeVisible({ timeout: 15_000 })
    const runningOverflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(runningOverflow).toBeLessThanOrEqual(1)

    const widths = await page.locator('.home-demo-frame').evaluate((frame) => ({
      client: frame.clientWidth,
      scroll: frame.scrollWidth,
    }))
    expect(widths.scroll - widths.client).toBeLessThanOrEqual(1)
    expect(failures).toEqual([])
  })
})

test('publishes direct release downloads and a large product share card', async ({ page }) => {
  await page.goto('./')

  await expect(page.getByTestId('download-macos-arm64')).toHaveAttribute(
    'href',
    'https://github.com/attson/atterm/releases/latest/download/AT-Term-darwin-arm64.zip',
  )
  await expect(page.getByTestId('download-linux-amd64')).toHaveAttribute(
    'href',
    'https://github.com/attson/atterm/releases/latest/download/AT-Term-linux-amd64.tar.gz',
  )
  await expect(page.getByTestId('download-windows-amd64')).toHaveAttribute(
    'href',
    'https://github.com/attson/atterm/releases/latest/download/AT-Term-windows-amd64.zip',
  )
  await expect(page.getByTestId('download-checksums')).toHaveAttribute(
    'href',
    'https://github.com/attson/atterm/releases/latest/download/SHA256SUMS',
  )

  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute(
    'content',
    'https://attson.github.io/atterm/atterm-demo-poster.png',
  )
  await expect(page.locator('meta[property="og:image:width"]')).toHaveAttribute('content', '1200')
  await expect(page.locator('meta[property="og:image:height"]')).toHaveAttribute('content', '630')
  await expect(page.locator('meta[name="twitter:card"]')).toHaveAttribute('content', 'summary_large_image')

  const posterSize = await page.locator('[data-testid="demo-poster"] img').evaluate((image) => {
    const element = image as HTMLImageElement
    return { width: element.naturalWidth, height: element.naturalHeight }
  })
  expect(posterSize).toEqual({ width: 1200, height: 630 })
})

test('serves a complete English locale with locale-specific metadata', async ({ page }) => {
  await page.goto('./en/')

  await expect(page.getByRole('heading', { level: 1 })).toContainText('AT Term')
  await expect(page.getByText('Choose how you connect')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Documentation' })).toHaveAttribute('href', '/atterm/en/guide/')
  await expect(page.getByRole('link', { name: 'Deploy your Relay' })).toHaveAttribute(
    'href',
    '/atterm/en/guide/deploy-relay.html',
  )
  await expect(page.locator('meta[property="og:locale"]')).toHaveAttribute('content', 'en_US')
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    'href',
    'https://attson.github.io/atterm/en/',
  )

  for (const route of [
    'guide/',
    'guide/remote-takeover.html',
    'guide/e2ee.html',
    'guide/deploy-relay.html',
    'guide/deploy-rendezvous.html',
    'guide/ai-agents.html',
    'guide/faq.html',
  ]) {
    const response = await page.goto(`./en/${route}`)
    expect(response?.ok(), route).toBe(true)
    await expect(page.locator('main h1')).toBeVisible()
  }
})

test('keeps the English hero within a medium desktop viewport', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 800 })
  await page.goto('./en/')

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
})

test('keeps the site canvas in sync with the selected light and dark theme', async ({ page }) => {
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme })
    await page.goto('./')
    await expect(page.getByTestId('demo-poster')).toBeVisible()

    const palette = await page.evaluate(() => {
      const probe = document.createElement('div')
      probe.style.cssText = [
        'position:fixed',
        'visibility:hidden',
        'background-color:var(--vp-c-bg)',
        'color:var(--vp-c-text-1)',
      ].join(';')
      document.body.appendChild(probe)

      const bodyStyle = getComputedStyle(document.body)
      const rootStyle = getComputedStyle(document.documentElement)
      const probeStyle = getComputedStyle(probe)
      const result = {
        isDark: document.documentElement.classList.contains('dark'),
        bodyBackground: bodyStyle.backgroundColor,
        bodyColor: bodyStyle.color,
        bodyFontSize: bodyStyle.fontSize,
        themeBackgroundToken: rootStyle.getPropertyValue('--vp-c-bg').trim(),
        themeColorToken: rootStyle.getPropertyValue('--vp-c-text-1').trim(),
        themeBackground: probeStyle.backgroundColor,
        themeColor: probeStyle.color,
      }
      probe.remove()
      return result
    })

    expect(palette.isDark, colorScheme).toBe(colorScheme === 'dark')
    expect(palette.themeBackgroundToken, colorScheme).not.toBe('')
    expect(palette.themeColorToken, colorScheme).not.toBe('')
    expect(palette.bodyBackground, colorScheme).toBe(palette.themeBackground)
    expect(palette.bodyColor, colorScheme).toBe(palette.themeColor)
    expect(palette.bodyFontSize, colorScheme).toBe('16px')
  }

  await page.locator('.VPSwitchAppearance:visible').first().click()
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  const switchedPalette = await page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.backgroundColor = 'var(--vp-c-bg)'
    document.body.appendChild(probe)
    const result = {
      body: getComputedStyle(document.body).backgroundColor,
      theme: getComputedStyle(probe).backgroundColor,
    }
    probe.remove()
    return result
  })
  expect(switchedPalette.body).toBe(switchedPalette.theme)
})
