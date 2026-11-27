<script setup lang="ts">
import { Pencil, RotateCcw, X } from "lucide-vue-next";
import type { FileTransferJob } from "../../lib/api";
import { useI18n } from "../../i18n/useI18n";

defineProps<{ jobs: FileTransferJob[] }>();
defineEmits<{
  (event: "cancel", id: string): void;
  (event: "retry", id: string): void;
  (event: "edit-host", hostId: string): void;
}>();

const { t } = useI18n();

function progress(job: FileTransferJob): number {
  if (job.bytes_total > 0) return Math.min(100, Math.round(job.bytes_done * 100 / job.bytes_total));
  if (job.entries_total > 0) return Math.min(100, Math.round(job.entries_done * 100 / job.entries_total));
  return job.state === "completed" ? 100 : 0;
}
</script>

<template>
  <section v-if="jobs.length" class="transfer-queue" aria-live="polite">
    <h2>{{ t("sftp.transferQueue") }}</h2>
    <div class="job-list">
      <div v-for="job in jobs" :key="job.id" class="transfer-job" :data-test="`transfer-job-${job.id}`">
        <div class="job-main">
          <span class="job-path" v-tooltip="job.current_path">{{ job.current_path || job.id }}</span>
          <span class="job-state">{{ t(`sftp.${job.state}`) }}</span>
          <span class="job-percent">{{ progress(job) }}%</span>
          <div class="job-actions">
            <button
              v-if="job.state === 'queued' || job.state === 'running'"
              :data-test="`cancel-job-${job.id}`"
              v-tooltip="t('sftp.cancelTransfer')"
              @click="$emit('cancel', job.id)"
            ><X :size="15" /></button>
            <button
              v-if="job.state === 'failed'"
              :data-test="`retry-job-${job.id}`"
              v-tooltip="t('common.retry')"
              @click="$emit('retry', job.id)"
            ><RotateCcw :size="15" /></button>
            <button
              v-if="job.state === 'failed' && job.credential_host_id"
              :data-test="`edit-host-${job.id}`"
              v-tooltip="t('sftp.editHost')"
              @click="$emit('edit-host', job.credential_host_id)"
            ><Pencil :size="15" /></button>
          </div>
        </div>
        <div class="progress-track"><span :style="{ width: `${progress(job)}%` }"></span></div>
        <p v-if="job.error" class="job-error">{{ job.error }}</p>
        <p v-for="warning in job.warnings" :key="warning" class="job-warning">{{ warning }}</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.transfer-queue { flex: 0 0 auto; border-top: 1px solid #30363d; background: #0d1117; padding: 8px 12px 10px; color: #c9d1d9; }
h2 { margin: 0 0 7px; font-size: 12px; letter-spacing: 0; color: #8b949e; }
.job-list { display: grid; gap: 7px; max-height: 150px; overflow: auto; }
.transfer-job { min-width: 0; }
.job-main { display: grid; grid-template-columns: minmax(90px, 1fr) auto 42px auto; align-items: center; gap: 8px; font-size: 12px; }
.job-path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #f0f6fc; }
.job-state, .job-percent { color: #8b949e; }
button { width: 26px; height: 24px; display: grid; place-content: center; border: 0; border-radius: 4px; background: transparent; color: #8b949e; cursor: pointer; }
button:hover { background: #21262d; color: #f0f6fc; }
.job-actions { min-width: 26px; display: flex; justify-content: flex-end; gap: 2px; }
.progress-track { height: 3px; margin-top: 4px; overflow: hidden; background: #21262d; }
.progress-track span { display: block; height: 100%; background: #2f81f7; transition: width .12s linear; }
.job-error, .job-warning { margin: 4px 0 0; font-size: 11px; }
.job-error { color: #ff7b72; }
.job-warning { color: #d29922; }
</style>
