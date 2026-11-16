<script setup lang="ts">
import { computed, ref, type Component } from "vue";
import { NConfigProvider, NMessageProvider, darkTheme } from "naive-ui";
import { ChartNoAxesCombined, KeyRound, MessageSquareText, SlidersHorizontal, UsersRound } from "lucide-vue-next";
import { getNaiveOverrides } from "@shared/theme/naive-theme";
import { naiveLocale } from "@shared/i18n/naive-locale";
import { useI18n } from "@shared/i18n/useI18n";
import Invitations from "./admin/Invitations.vue";
import Users from "./admin/Users.vue";
import Config from "./admin/Config.vue";
import FeishuConfig from "./admin/FeishuConfig.vue";
import Traffic from "./admin/Traffic.vue";

type AdminTabKey = "invitations" | "users" | "config" | "feishu" | "traffic";

const { t } = useI18n();

// naiveLocale is a shared ComputedRef<{ locale, dateLocale }>. vue-tsc's
// template type checker doesn't auto-unwrap nested access on a ComputedRef
// (`naiveLocale.locale` errors under strict template checking), so expose
// the two inner fields as top-level computeds here so the template can bind
// them directly.
const naiveLocaleValue = computed(() => naiveLocale.value.locale);
const naiveDateLocaleValue = computed(() => naiveLocale.value.dateLocale);

// Not persisted: reopening the admin view always starts on "invitations".
const active = ref<AdminTabKey>("invitations");

const tabs = computed<{ key: AdminTabKey; label: string; icon: Component }[]>(() => [
  { key: "invitations", label: t("admin.invitations"), icon: KeyRound },
  { key: "users", label: t("admin.users"), icon: UsersRound },
  { key: "config", label: t("admin.configTab"), icon: SlidersHorizontal },
  { key: "feishu", label: t("admin.feishuTab"), icon: MessageSquareText },
  { key: "traffic", label: t("admin.trafficTab"), icon: ChartNoAxesCombined },
]);

// All four admin tab components consume `useMessage()` from naive-ui, which
// throws unconditionally when no <n-message-provider> is above them in the
// tree. The old standalone web/src/admin/App.vue wrapped its tabs in
// <n-config-provider> + <n-message-provider>; when the admin surface moved
// into desktop/frontend the wrapper was lost, so mounting AdminPanel died on
// first render. Re-establish the same provider shell here, and reuse the
// shared token overrides + locale so the naive UI theme matches the rest of
// the app.
const overrides = getNaiveOverrides();
</script>

<template>
  <div class="admin-panel">
    <n-config-provider
      abstract
      :theme="darkTheme"
      :theme-overrides="overrides"
      :locale="naiveLocaleValue"
      :date-locale="naiveDateLocaleValue"
    >
      <n-message-provider>
        <div class="admin-tabs-shell">
          <nav class="admin-tabs" role="tablist" :aria-label="t('admin.title')">
            <button
              v-for="tab in tabs"
              :key="tab.key"
              type="button"
              role="tab"
              :aria-selected="active === tab.key"
              @click="active = tab.key"
              :class="{ active: active === tab.key }"
              :data-test="`admin-tab-${tab.key}`"
            >
              <component :is="tab.icon" :size="15" aria-hidden="true" />
              <span>{{ tab.label }}</span>
            </button>
          </nav>
        </div>
        <div class="admin-body">
          <Invitations v-if="active === 'invitations'" />
          <Users v-if="active === 'users'" />
          <Config v-if="active === 'config'" />
          <FeishuConfig v-if="active === 'feishu'" />
          <Traffic v-if="active === 'traffic'" />
        </div>
      </n-message-provider>
    </n-config-provider>
  </div>
</template>

<style scoped>
.admin-panel {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}
.admin-tabs-shell {
  flex: 0 0 auto;
  padding: 8px clamp(16px, 2.5vw, 36px);
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg) 92%, var(--panel));
}
.admin-tabs {
  width: 100%;
  max-width: 1540px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}
.admin-tabs::-webkit-scrollbar { display: none; }
.admin-tabs button {
  height: 36px;
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 0 12px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  font-size: 12px;
  letter-spacing: 0;
}
.admin-tabs button:hover {
  color: var(--fg);
  background: color-mix(in srgb, var(--panel) 72%, transparent);
}
.admin-tabs button.active {
  color: var(--fg);
  border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  background: color-mix(in srgb, var(--accent) 12%, var(--panel));
  font-weight: 600;
}
.admin-tabs button.active svg { color: var(--accent); }
.admin-tabs button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.admin-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 22px clamp(16px, 2.5vw, 36px) 40px;
  scrollbar-gutter: stable;
}
.admin-body :deep(.admin-page) {
  width: 100%;
  max-width: 1280px;
  margin: 0 auto;
  color: var(--fg);
}
.admin-body :deep(.admin-page-header) {
  min-height: 54px;
  margin-bottom: 14px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}
.admin-body :deep(.admin-page-header h1) {
  margin: 0;
  font-size: 18px;
  line-height: 1.35;
  font-weight: 650;
  letter-spacing: 0;
}
.admin-body :deep(.admin-page-header p) {
  max-width: 780px;
  margin: 6px 0 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.6;
}
.admin-body :deep(.admin-surface) {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.admin-body :deep(.admin-table-surface) {
  overflow: hidden;
}
@media (max-width: 640px) {
  .admin-tabs-shell { padding-inline: 10px; }
  .admin-tabs button { height: 34px; padding-inline: 10px; }
  .admin-body { padding: 16px 12px 32px; }
  .admin-body :deep(.admin-page-header) { min-height: 0; }
}
</style>
