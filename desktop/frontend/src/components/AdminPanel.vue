<script setup lang="ts">
import { computed, ref } from "vue";
import { NConfigProvider, NMessageProvider, darkTheme } from "naive-ui";
import { getNaiveOverrides } from "@shared/theme/naive-theme";
import { naiveLocale } from "@shared/i18n/naive-locale";
import { useI18n } from "@shared/i18n/useI18n";
import Invitations from "./admin/Invitations.vue";
import Users from "./admin/Users.vue";
import Config from "./admin/Config.vue";
import FeishuConfig from "./admin/FeishuConfig.vue";

type AdminTabKey = "invitations" | "users" | "config" | "feishu";

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

const tabs = computed<{ key: AdminTabKey; label: string }[]>(() => [
  { key: "invitations", label: t("admin.invitations") },
  { key: "users", label: t("admin.users") },
  { key: "config", label: t("admin.configTab") },
  { key: "feishu", label: t("admin.feishuTab") },
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
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocaleValue"
    :date-locale="naiveDateLocaleValue"
  >
    <n-message-provider>
      <div class="admin-panel">
        <div class="admin-tabs">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            @click="active = tab.key"
            :class="{ active: active === tab.key }"
            :data-test="`admin-tab-${tab.key}`"
          >
            {{ tab.label }}
          </button>
        </div>
        <div class="admin-body">
          <Invitations v-if="active === 'invitations'" />
          <Users v-if="active === 'users'" />
          <Config v-if="active === 'config'" />
          <FeishuConfig v-if="active === 'feishu'" />
        </div>
      </div>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.admin-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.admin-tabs {
  display: flex;
  gap: 2px;
  padding: 8px 12px 0;
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
}
.admin-tabs button {
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--fg-dim);
  padding: 8px 12px;
  cursor: pointer;
  font-size: 13px;
}
.admin-tabs button:hover {
  color: var(--fg);
}
.admin-tabs button.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: 600;
}
.admin-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 20px 24px;
}
</style>
