<template>
  <div class="mobile-placeholder" data-testid="mobile-placeholder">
    <h1>AT Term</h1>
    <p class="lead">Mobile shell loaded.</p>
    <p class="muted">
      Relay configuration UI ships in PR-C. For now this confirms the
      Capacitor bundle and the desktop frontend's <code>platform/</code>
      adapter wire up end-to-end inside iOS WebView.
    </p>
  </div>
</template>

<style scoped>
.mobile-placeholder {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 2rem 1.25rem;
  text-align: center;
  background: #05070d;
  color: #e6e7ea;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}
.mobile-placeholder h1 { margin: 0 0 1rem; font-size: 1.75rem; font-weight: 600; }
.mobile-placeholder .lead { margin: 0 0 0.75rem; font-size: 1rem; }
.mobile-placeholder .muted { margin: 0; max-width: 32rem; font-size: 0.875rem; color: #8d93a3; line-height: 1.5; }
.mobile-placeholder code { background: rgba(255,255,255,0.08); padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.8125rem; }
</style>
