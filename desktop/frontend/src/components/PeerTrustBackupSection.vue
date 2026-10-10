<script lang="ts" setup>
import { computed, ref } from 'vue'
import { Download, KeyRound, Upload } from 'lucide-vue-next'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import type { PeerSpaceStatus } from '../platform/types'

const props = defineProps<{ configured: boolean }>()
const emit = defineEmits<{ restored: [status: PeerSpaceStatus] }>()
const { t } = useI18n()
const platform = usePlatform()

const passphrase = ref('')
const confirmation = ref('')
const selectedFile = ref<File | null>(null)
const busy = ref<'export' | 'import' | ''>('')
const success = ref('')
const error = ref('')
const passphraseLength = computed(() => Array.from(passphrase.value).length)

const canExport = computed(() => (
  Boolean(platform.peer?.exportTrustBackup)
  && passphraseLength.value >= 12
  && passphrase.value === confirmation.value
  && !busy.value
))
const canImport = computed(() => (
  Boolean(platform.peer?.importTrustBackup)
  && Boolean(selectedFile.value)
  && passphraseLength.value >= 12
  && !busy.value
))

async function exportBackup(): Promise<void> {
  const run = platform.peer?.exportTrustBackup
  if (!run || !canExport.value) return
  error.value = ''
  success.value = ''
  busy.value = 'export'
  try {
    const path = await run(passphrase.value)
    if (path) {
      success.value = t('settings.peer.backup.exported', { path })
      passphrase.value = ''
      confirmation.value = ''
    }
  } catch {
    error.value = t('settings.peer.backup.exportFailed')
  } finally {
    busy.value = ''
  }
}

function selectFile(event: Event): void {
  const input = event.target as HTMLInputElement
  selectedFile.value = input.files?.[0] ?? null
  error.value = ''
  success.value = ''
}

async function importBackup(): Promise<void> {
  const run = platform.peer?.importTrustBackup
  const file = selectedFile.value
  if (!run || !file || !canImport.value) return
  error.value = ''
  success.value = ''
  busy.value = 'import'
  try {
    if (file.size > 8 * 1024 * 1024) throw new Error('backup too large')
    const restored = await run(await file.text(), passphrase.value)
    passphrase.value = ''
    selectedFile.value = null
    emit('restored', restored)
  } catch {
    error.value = t('settings.peer.backup.importFailed')
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <section class="peer-section trust-backup" data-testid="peer-trust-backup">
    <div class="section-heading">
      <KeyRound :size="17" aria-hidden="true" />
      <div>
        <h3>{{ t('settings.peer.backup.title') }}</h3>
        <p class="hint">{{ t(configured ? 'settings.peer.backup.exportHint' : 'settings.peer.backup.importHint') }}</p>
      </div>
    </div>

    <template v-if="configured">
      <div class="backup-fields">
        <div class="form-field">
          <label class="field-label" for="peer-backup-passphrase">{{ t('settings.peer.backup.passphrase') }}</label>
          <input
            id="peer-backup-passphrase"
            v-model="passphrase"
            data-testid="peer-backup-passphrase"
            type="password"
            minlength="12"
            autocomplete="new-password"
            :disabled="Boolean(busy)"
          />
        </div>
        <div class="form-field">
          <label class="field-label" for="peer-backup-confirmation">{{ t('settings.peer.backup.confirmPassphrase') }}</label>
          <input
            id="peer-backup-confirmation"
            v-model="confirmation"
            data-testid="peer-backup-confirmation"
            type="password"
            minlength="12"
            autocomplete="new-password"
            :disabled="Boolean(busy)"
          />
        </div>
      </div>
      <p v-if="confirmation && passphrase !== confirmation" class="inline-error">
        {{ t('settings.peer.backup.mismatch') }}
      </p>
      <button
        type="button"
        class="secondary-action"
        data-testid="peer-backup-export"
        :disabled="!canExport"
        @click="exportBackup"
      >
        <Download :size="15" aria-hidden="true" />
        {{ busy === 'export' ? t('settings.peer.backup.exporting') : t('settings.peer.backup.export') }}
      </button>
    </template>

    <template v-else>
      <label class="file-picker secondary-action" :class="{ disabled: Boolean(busy) }">
        <Upload :size="15" aria-hidden="true" />
        <span>{{ selectedFile?.name || t('settings.peer.backup.chooseFile') }}</span>
        <input
          data-testid="peer-backup-file"
          type="file"
          accept="application/json,.json"
          :disabled="Boolean(busy)"
          @change="selectFile"
        />
      </label>
      <div class="form-field">
        <label class="field-label" for="peer-backup-import-passphrase">{{ t('settings.peer.backup.passphrase') }}</label>
        <input
          id="peer-backup-import-passphrase"
          v-model="passphrase"
          data-testid="peer-backup-import-passphrase"
          type="password"
          minlength="12"
          autocomplete="current-password"
          :disabled="Boolean(busy)"
        />
      </div>
      <p class="hint">{{ t('settings.peer.backup.restoreWarning') }}</p>
      <button
        type="button"
        class="primary-action"
        data-testid="peer-backup-import"
        :disabled="!canImport"
        @click="importBackup"
      >
        <Upload :size="15" aria-hidden="true" />
        {{ busy === 'import' ? t('settings.peer.backup.importing') : t('settings.peer.backup.import') }}
      </button>
    </template>

    <p v-if="success" class="success-note" data-testid="peer-backup-success">{{ success }}</p>
    <p v-if="error" class="inline-error" role="alert" data-testid="peer-backup-error">{{ error }}</p>
  </section>
</template>

<style scoped>
.trust-backup {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.section-heading h3,
.section-heading p {
  margin: 0;
}

.section-heading h3 {
  color: var(--fg);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0;
}

.hint {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.45;
}

.backup-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.form-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.field-label {
  font-size: 12px;
  color: var(--fg-dim);
}

input[type='password'] {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 7px 9px;
  color: var(--fg);
  background: var(--bg);
}

.file-picker {
  width: fit-content;
  max-width: 100%;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}

.file-picker span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-picker input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.file-picker.disabled {
  opacity: 0.55;
  cursor: default;
}

.primary-action,
.secondary-action {
  width: fit-content;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 32px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.primary-action {
  border-color: var(--accent);
  background: var(--accent);
  color: #0d1117;
  font-weight: 600;
}

.secondary-action {
  background: transparent;
  color: var(--fg);
}

.inline-error {
  margin: 0;
  color: var(--danger-color, #c2413b);
  font-size: 12px;
}

.success-note {
  margin: 0;
  color: var(--success-color, #278552);
  font-size: 12px;
  overflow-wrap: anywhere;
}

@media (max-width: 560px) {
  .backup-fields {
    grid-template-columns: 1fr;
  }
}
</style>
