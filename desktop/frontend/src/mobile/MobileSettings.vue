<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { type LocalePreference } from '../i18n'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { effectiveTemplates, type QuickTemplate } from '../lib/templates'
import { effectiveAuxKeys, parseSeq, displaySeq, type AuxKey } from '../lib/auxKeys'
import MobileListEditor, { type EditorRow } from './MobileListEditor.vue'

const emit = defineEmits<{ (e: 'back'): void; (e: 'logout'): void }>()

const platform = usePlatform()
const { t, languageOptions, localePreference, setLocalePreference } = useI18n()

const templateRows = ref<EditorRow[]>([])
const auxRows = ref<EditorRow[]>([])

const localizedLanguageOptions = computed(() =>
  languageOptions.map((o) => ({ value: o.value, label: t(o.labelKey) })),
)

async function loadTemplates() {
  const list = await effectiveTemplates(platform.templates)
  templateRows.value = list.map((x) => ({ id: x.id, label: x.label, value: x.text }))
}
async function loadAux() {
  const list = await effectiveAuxKeys(platform.auxKeys)
  auxRows.value = list.map((x) => ({ id: x.id, label: x.label, value: x.seq }))
}

onMounted(async () => { await loadTemplates(); await loadAux() })

async function onTemplatesChange(rows: EditorRow[]) {
  templateRows.value = rows
  const list: QuickTemplate[] = rows.map((r) => ({ id: r.id, label: r.label, text: r.value }))
  await platform.templates.save(list)
}
async function onTemplatesReset() {
  await platform.templates.clear()
  await loadTemplates()
}

async function onAuxChange(rows: EditorRow[]) {
  auxRows.value = rows
  const list: AuxKey[] = rows.map((r) => ({ id: r.id, label: r.label, seq: r.value }))
  await platform.auxKeys.save(list)
}
async function onAuxReset() {
  await platform.auxKeys.clear()
  await loadAux()
}

async function onLanguageChange(e: Event) {
  await setLocalePreference((e.target as HTMLSelectElement).value as LocalePreference)
}
</script>

<template>
  <div class="settings">
    <header class="bar">
      <button class="back" data-testid="settings-back" @click="emit('back')">{{ t('mobile.settings.back') }}</button>
      <h1>{{ t('mobile.settings.title') }}</h1>
      <span class="spacer" />
    </header>

    <section class="block">
      <label class="field">
        <span class="ftitle">{{ t('mobile.settings.language') }}</span>
        <select data-testid="settings-language" :value="localePreference" @change="onLanguageChange">
          <option v-for="o in localizedLanguageOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>
    </section>

    <section class="block">
      <h2>{{ t('mobile.settings.templates') }}</h2>
      <MobileListEditor
        :rows="templateRows"
        testid="settings-templates"
        :label-text="t('mobile.settings.label')"
        :value-text="t('mobile.settings.text')"
        :add-text="t('mobile.settings.add')"
        :edit-text="t('mobile.settings.edit')"
        :delete-text="t('mobile.settings.delete')"
        :reset-text="t('mobile.settings.reset')"
        :cancel-text="t('common.cancel')"
        :save-text="t('mobile.settings.save')"
        :reset-confirm-text="t('mobile.settings.resetConfirm')"
        @update:rows="onTemplatesChange"
        @reset="onTemplatesReset"
      />
    </section>

    <section class="block">
      <h2>{{ t('mobile.settings.auxKeys') }}</h2>
      <MobileListEditor
        :rows="auxRows"
        testid="settings-auxkeys"
        :label-text="t('mobile.settings.label')"
        :value-text="t('mobile.settings.seq')"
        :add-text="t('mobile.settings.add')"
        :edit-text="t('mobile.settings.edit')"
        :delete-text="t('mobile.settings.delete')"
        :reset-text="t('mobile.settings.reset')"
        :cancel-text="t('common.cancel')"
        :save-text="t('mobile.settings.save')"
        :reset-confirm-text="t('mobile.settings.resetConfirm')"
        :display-value="displaySeq"
        :parse-value="parseSeq"
        @update:rows="onAuxChange"
        @reset="onAuxReset"
      />
    </section>

    <section class="block">
      <button class="logout" data-testid="settings-logout" @click="emit('logout')">
        {{ t('mobile.settings.logout') }}
      </button>
    </section>
  </div>
</template>

<style scoped>
.settings { min-height: 100vh; box-sizing: border-box; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: var(--font-sans); padding: calc(env(safe-area-inset-top)) 1rem calc(2rem + env(safe-area-inset-bottom)); }
.bar { display: flex; align-items: center; gap: 8px; padding: 0.75rem 0; position: sticky; top: 0; background: var(--bg, #05070d); }
.bar h1 { font-size: 1.1rem; margin: 0; flex: 1; text-align: center; }
.back { height: 36px; padding: 0 12px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #cbd5e1; font-size: 0.85rem; }
.spacer { width: 56px; }
.block { margin-bottom: 1.5rem; }
.block h2 { font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; color: #8d93a3; margin: 0 0 0.6rem; }
.field { display: block; }
.ftitle { display: block; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; color: #8d93a3; margin-bottom: 0.6rem; }
.field select { width: 100%; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; font-family: var(--font-sans); }
.logout { width: 100%; height: 46px; border-radius: 10px; border: 1px solid rgba(248,113,113,.4); background: rgba(248,113,113,.1); color: #f87171; font-size: 1rem; font-weight: 600; }
</style>
