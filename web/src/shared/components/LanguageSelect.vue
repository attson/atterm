<script setup lang="ts">
import { useI18n } from '@shared/i18n/useI18n'
import type { LocalePreference } from '@shared/i18n'

const { t, languageOptions, localePreference, setLocalePreference } = useI18n()

async function onChange(event: Event) {
  await setLocalePreference((event.target as HTMLSelectElement).value as LocalePreference)
}
</script>

<template>
  <label class="language-select">
    <span>{{ t('common.language') }}</span>
    <select data-testid="language-select" :value="localePreference" @change="onChange">
      <option
        v-for="option in languageOptions"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
  </label>
</template>

<style scoped>
.language-select {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--fg-dim);
  font-size: 0.875rem;
}

.language-select select {
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--panel);
  color: var(--fg);
  padding: 0.35rem 0.5rem;
}
</style>
