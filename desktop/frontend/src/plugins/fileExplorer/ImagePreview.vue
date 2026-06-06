<script lang="ts" setup>
defineProps<{ path: string; theme: "dimmed" | "light" }>();
</script>
<template><div class="placeholder">image preview placeholder</div></template>
<style scoped>.placeholder { padding: 18px; color: var(--ed-muted, #888); }</style>
