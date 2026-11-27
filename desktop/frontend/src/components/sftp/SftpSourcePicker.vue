<script setup lang="ts">
import { computed } from "vue";
import type { SSHHost } from "../../lib/api";
import { useI18n } from "../../i18n/useI18n";
import SelectDropdown, { type SelectOption } from "../SelectDropdown.vue";
import type { SftpSource } from "./sftpWorkspaceModel";

const props = defineProps<{
  source: SftpSource;
  hosts: SSHHost[];
  disabled?: boolean;
}>();

const emit = defineEmits<{
  (event: "select", source: SftpSource): void;
}>();

const { t } = useI18n();

const modelValue = computed(() => props.source.kind === "local" ? "local" : `ssh:${props.source.hostId}`);
const options = computed<SelectOption[]>(() => [
  { value: "local", label: t("sftp.localMachine") },
  ...props.hosts.map((host) => ({
    value: `ssh:${host.id}`,
    label: host.alias?.trim() || host.host,
    description: `${host.user}@${host.host}`,
  })),
]);

function onSelect(value: string) {
  if (value === "local") {
    emit("select", { kind: "local" });
    return;
  }
  emit("select", { kind: "ssh", hostId: value.slice(4) });
}
</script>

<template>
  <SelectDropdown
    data-test="source-picker"
    :model-value="modelValue"
    :options="options"
    :disabled="disabled"
    :aria-label="t('sftp.source')"
    @update:model-value="onSelect"
  />
</template>
