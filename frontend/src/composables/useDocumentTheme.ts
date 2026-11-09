import { onBeforeUnmount, onMounted, ref } from 'vue'
import { setTheme } from '@/utils/theme'

/** Observe the applied document theme so controls also reflect other tabs. */
export function useDocumentTheme() {
  const isDark = ref(document.documentElement.classList.contains('dark'))
  const sync = () => { isDark.value = document.documentElement.classList.contains('dark') }
  let observer: MutationObserver | undefined

  onMounted(() => {
    sync()
    observer = new MutationObserver(sync)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  })
  onBeforeUnmount(() => observer?.disconnect())

  function toggleTheme() {
    setTheme(document.documentElement.classList.contains('dark') ? 'light' : 'dark')
    sync()
  }

  return { isDark, toggleTheme }
}
