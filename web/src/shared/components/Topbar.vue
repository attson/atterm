<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import type { MeResponse } from '@shared/api/types'
import { getMe } from '@shared/api/me'
import { logout } from '@shared/api/auth'
import { fetchVersion, formatVersionLabel } from '@shared/api/version'
import { useI18n } from '@shared/i18n/useI18n'

defineProps<{ active: 'home' | 'settings' | 'admin' }>()

const me = ref<MeResponse | null>(null)
const version = ref('dev')
const { t } = useI18n()
const versionLabel = computed(() => formatVersionLabel(version.value, t))

onMounted(async () => {
  try {
    me.value = await getMe()
  } catch {
    // apiFetch handles 401 by redirecting; nothing else to do here.
  }
  try {
    version.value = await fetchVersion()
  } catch {
    // Keep the translated fallback label.
  }
})

async function onLogout() {
  try {
    await logout()
  } catch {
    // The session may already be gone; still navigate to login.
  } finally {
    location.assign('/login.html')
  }
}
</script>

<template>
  <header class="topbar">
    <div class="brand-block">
      <div class="brand">{{ t('common.appName') }}</div>
      <div class="version">{{ versionLabel }}</div>
    </div>
    <nav class="topnav" :aria-label="t('topbar.primaryNav')">
      <a
        href="/"
        :class="{ active: active === 'home' }"
        :aria-current="active === 'home' ? 'page' : false"
      >{{ t('topbar.home') }}</a>
      <a
        href="/settings.html"
        :class="{ active: active === 'settings' }"
        :aria-current="active === 'settings' ? 'page' : false"
      >{{ t('topbar.settings') }}</a>
      <a
        v-if="me?.is_admin"
        href="/admin/"
        :class="{ active: active === 'admin' }"
        :aria-current="active === 'admin' ? 'page' : false"
      >{{ t('topbar.admin') }}</a>
    </nav>
    <button type="button" class="ghost-btn" @click="onLogout">{{ t('topbar.signOut') }}</button>
  </header>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.5rem;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  color: var(--fg);
}
.brand-block { display: flex; flex-direction: column; gap: 0.125rem; }
.brand { font-weight: 700; letter-spacing: 0.08em; font-size: 1rem; }
.version { color: var(--fg-dim); font-size: 0.75rem; }
.topnav { display: flex; gap: 1.25rem; }
.topnav a {
  color: var(--fg-dim);
  text-decoration: none;
  font-weight: 600;
  font-size: 0.875rem;
  letter-spacing: 0.04em;
}
.topnav a.active { color: var(--accent); }
.topnav a:hover { color: var(--accent); }
.ghost-btn {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0.5rem 1rem;
  cursor: pointer;
  font: inherit;
  font-size: 0.875rem;
  font-weight: 600;
}
.ghost-btn:hover { border-color: var(--accent); color: var(--accent); }
</style>
