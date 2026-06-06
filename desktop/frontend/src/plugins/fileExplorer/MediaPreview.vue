<script lang="ts" setup>
defineProps<{ path: string; kind: "audio" | "video" }>();
</script>
<template><div class="placeholder">media preview placeholder</div></template>
<style scoped>.placeholder { padding: 18px; color: var(--ed-muted, #888); }</style>
