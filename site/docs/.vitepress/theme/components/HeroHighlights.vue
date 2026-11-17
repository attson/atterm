<script setup lang="ts">
import { computed } from 'vue'
import { useData } from 'vitepress'

const { frontmatter } = useData()

const highlights = computed<string[]>(() => frontmatter.value.hero?.highlights ?? [])
const highlightsLabel = computed<string>(() => frontmatter.value.hero?.highlightsLabel ?? 'Product highlights')
</script>

<template>
  <ul v-if="highlights.length" class="hero-highlights" :aria-label="highlightsLabel">
    <li v-for="highlight in highlights" :key="highlight">{{ highlight }}</li>
  </ul>
</template>

<style scoped>
.hero-highlights {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
}

.hero-highlights li {
  padding: 5px 9px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  color: var(--vp-c-text-2);
  background: var(--vp-c-bg-soft);
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
}

@media (max-width: 639px) {
  .hero-highlights {
    gap: 6px;
    margin-top: 18px;
  }

  .hero-highlights li {
    padding: 4px 8px;
    font-size: 12px;
  }
}
</style>
