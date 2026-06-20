<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NTabs,
  NTabPane,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import Topbar from '@shared/components/Topbar.vue'
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { isMobileApp } from '@shared/api/relay-config'
import { useI18n } from '@shared/i18n/useI18n'
import { naiveLocale } from '@shared/i18n/naive-locale'
import ChangePassword from './tabs/ChangePassword.vue'
import Sessions from './tabs/Sessions.vue'
import Notifications from './tabs/Notifications.vue'
import DangerZone from './tabs/DangerZone.vue'
import Relay from './tabs/Relay.vue'

const mobile = isMobileApp()
const { t } = useI18n()

const TAB_NAMES = mobile
  ? (['relay'] as const)
  : (['change-password', 'sessions', 'notifications', 'danger'] as const)
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return (TAB_NAMES as readonly string[]).includes(h) ? (h as TabName) : TAB_NAMES[0]
}

const activeTab = ref<TabName>(nameFromHash())

function onHashChange() {
  activeTab.value = nameFromHash()
}

onMounted(() => window.addEventListener('hashchange', onHashChange))
onUnmounted(() => window.removeEventListener('hashchange', onHashChange))

function onTabChange(name: string) {
  if (!(TAB_NAMES as readonly string[]).includes(name)) return
  if (location.hash.replace(/^#/, '') !== name) {
    location.hash = '#' + name
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale.locale"
    :date-locale="naiveLocale.dateLocale"
  >
    <n-message-provider>
      <Topbar active="settings" />
      <main class="settings-page">
        <section class="language-settings" aria-labelledby="language-settings-title">
          <div>
            <h2 id="language-settings-title">{{ t('settings.language') }}</h2>
            <p>{{ t('settings.languageHint') }}</p>
          </div>
          <LanguageSelect />
        </section>
        <n-tabs
          :value="activeTab"
          type="line"
          animated
          @update:value="onTabChange"
        >
          <template v-if="mobile">
            <n-tab-pane name="relay" :tab="t('settings.tabs.relay')">
              <Relay />
            </n-tab-pane>
          </template>
          <template v-else>
            <n-tab-pane name="change-password" :tab="t('settings.tabs.changePassword')">
              <ChangePassword />
            </n-tab-pane>
            <n-tab-pane name="sessions" :tab="t('settings.tabs.sessions')">
              <Sessions />
            </n-tab-pane>
            <n-tab-pane name="notifications" :tab="t('settings.tabs.notifications')">
              <Notifications />
            </n-tab-pane>
            <n-tab-pane name="danger" :tab="t('settings.tabs.danger')">
              <DangerZone />
            </n-tab-pane>
          </template>
        </n-tabs>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.settings-page {
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  color: var(--fg);
  min-height: calc(100vh - 80px);
}
.language-settings {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.5rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  background: var(--panel);
}
.language-settings h2 {
  margin: 0;
  font-size: 1rem;
}
.language-settings p {
  margin: 0.25rem 0 0;
  color: var(--fg-dim);
}
</style>
