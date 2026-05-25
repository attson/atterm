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
import { useI18n } from '@shared/i18n/useI18n'
import { naiveLocale } from '@shared/i18n/naive-locale'
import Invitations from './tabs/Invitations.vue'
import Users from './tabs/Users.vue'
import Config from './tabs/Config.vue'

const TAB_NAMES = ['invitations', 'users', 'config'] as const
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return TAB_NAMES.includes(h as TabName) ? (h as TabName) : 'invitations'
}

const activeTab = ref<TabName>(nameFromHash())
const { t } = useI18n()

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
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale.locale"
    :date-locale="naiveLocale.dateLocale"
  >
    <n-message-provider>
      <Topbar active="admin" />
      <main class="admin-page">
        <n-tabs
          :value="activeTab"
          type="line"
          animated
          @update:value="onTabChange"
        >
          <n-tab-pane name="invitations" :tab="t('admin.invitations')">
            <Invitations />
          </n-tab-pane>
          <n-tab-pane name="users" :tab="t('admin.users')">
            <Users />
          </n-tab-pane>
          <n-tab-pane name="config" :tab="t('admin.configTab')">
            <Config />
          </n-tab-pane>
        </n-tabs>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.admin-page {
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  color: var(--fg);
  min-height: calc(100vh - 80px);
}
</style>
