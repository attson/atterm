import { computed } from 'vue'
import { dateEnUS, dateZhCN, enUS, zhCN } from 'naive-ui'
import { resolvedLocale } from './index'

export const naiveLocale = computed(() => (
  resolvedLocale.value === 'zh-CN'
    ? { locale: zhCN, dateLocale: dateZhCN }
    : { locale: enUS, dateLocale: dateEnUS }
))
