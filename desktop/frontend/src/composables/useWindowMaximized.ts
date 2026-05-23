import { ref, type Ref } from 'vue'
import { usePlatform } from '../platform'

const isMaximized = ref(false)

let initStarted = false
function initOnce() {
  if (initStarted) return
  initStarted = true
  Promise.resolve()
    .then(() => {
      const platform = usePlatform()
      const fn = platform.system.windowIsMaximized
      return fn ? fn() : Promise.resolve(false)
    })
    .then((v) => {
      isMaximized.value = !!v
    })
    .catch(() => {
      isMaximized.value = false
    })
}

export function useWindowMaximized(): Ref<boolean> {
  initOnce()
  return isMaximized
}

export function setMaximized(v: boolean): void {
  isMaximized.value = v
}
