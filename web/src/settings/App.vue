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
import ApiTokens from './tabs/ApiTokens.vue'
import ChangePassword from './tabs/ChangePassword.vue'
import Sessions from './tabs/Sessions.vue'
import Notifications from './tabs/Notifications.vue'
import DangerZone from './tabs/DangerZone.vue'

const TAB_NAMES = ['api-tokens', 'change-password', 'sessions', 'notifications', 'danger'] as const
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return TAB_NAMES.includes(h as TabName) ? (h as TabName) : 'api-tokens'
}

const activeTab = ref<TabName>(nameFromHash())

function onHashChange() {
  activeTab.value = nameFromHash()
}

onMounted(() => window.addEventListener('hashchange', onHashChange))
onUnmounted(() => window.removeEventListener('hashchange', onHashChange))

function onTabChange(name: string) {
  if (!TAB_NAMES.includes(name as TabName)) return
  if (location.hash.replace(/^#/, '') !== name) {
    location.hash = '#' + name
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <Topbar active="settings" />
      <main class="settings-page">
        <n-tabs
          :value="activeTab"
          type="line"
          animated
          @update:value="onTabChange"
        >
          <n-tab-pane name="api-tokens" tab="API Tokens">
            <ApiTokens />
          </n-tab-pane>
          <n-tab-pane name="change-password" tab="Change Password">
            <ChangePassword />
          </n-tab-pane>
          <n-tab-pane name="sessions" tab="Signed-in devices">
            <Sessions />
          </n-tab-pane>
          <n-tab-pane name="notifications" tab="Notifications">
            <Notifications />
          </n-tab-pane>
          <n-tab-pane name="danger" tab="Danger zone">
            <DangerZone />
          </n-tab-pane>
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
</style>
