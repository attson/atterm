<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NButton, NButtonGroup, NDataTable, NSpin, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { getTrafficStats } from '@shared/api/admin'
import type { AdminTrafficRow, TrafficView } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'

const { t } = useI18n()
const message = useMessage()

const view = ref<TrafficView>('group')
const rows = ref<AdminTrafficRow[]>([])
const loading = ref(false)

function humanBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

async function load() {
  loading.value = true
  try {
    const resp = await getTrafficStats({ view: view.value })
    rows.value = resp.rows
  } catch (e) {
    if (e instanceof ApiError) message.error(t('admin.traffic.loadFailed'))
    rows.value = []
  } finally {
    loading.value = false
  }
}

function setView(v: TrafficView) {
  if (view.value === v) return
  view.value = v
  load()
}

const columns = computed<DataTableColumns<AdminTrafficRow>>(() => {
  const cols: DataTableColumns<AdminTrafficRow> = [
    {
      title: t('admin.traffic.colUser'),
      key: 'user',
      render: (r) => r.email || r.user_id,
    },
  ]
  if (view.value === 'detail') {
    cols.push({
      title: t('admin.traffic.colFrameType'),
      key: 'ft',
      render: (r) => r.frame_type_name || String(r.frame_type ?? ''),
    })
  } else if (view.value === 'group') {
    cols.push({
      title: t('admin.traffic.colCategory'),
      key: 'cat',
      render: (r) => r.category || '',
    })
  }
  cols.push(
    {
      title: t('admin.traffic.colDirection'),
      key: 'dir',
      render: (r) => (r.direction === 0 ? t('admin.traffic.dirIn') : t('admin.traffic.dirOut')),
    },
    {
      title: t('admin.traffic.colBytes'),
      key: 'bytes',
      render: (r) => humanBytes(r.bytes),
    },
    {
      title: t('admin.traffic.colFrames'),
      key: 'frames',
      render: (r) => r.frames,
    },
  )
  return cols
})

function rowKey(r: AdminTrafficRow): string {
  return `${r.user_id}-${r.frame_type ?? r.category ?? ''}-${r.direction}`
}

onMounted(load)
</script>

<template>
  <div class="traffic">
    <n-button-group>
      <n-button :type="view === 'detail' ? 'primary' : 'default'" @click="setView('detail')">
        {{ t('admin.traffic.viewDetail') }}
      </n-button>
      <n-button :type="view === 'group' ? 'primary' : 'default'" @click="setView('group')">
        {{ t('admin.traffic.viewGroup') }}
      </n-button>
      <n-button :type="view === 'summary' ? 'primary' : 'default'" @click="setView('summary')">
        {{ t('admin.traffic.viewSummary') }}
      </n-button>
    </n-button-group>
    <n-spin :show="loading">
      <n-data-table
        v-if="rows.length"
        :columns="columns"
        :data="rows"
        :row-key="rowKey"
        data-test="traffic-table"
      />
      <p v-else class="empty" data-test="traffic-empty">{{ t('admin.traffic.empty') }}</p>
    </n-spin>
  </div>
</template>

<style scoped>
.traffic {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.empty {
  color: var(--fg-dim);
}
</style>
