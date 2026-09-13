<template>
  <div class="public-docs-content" :class="{ 'has-chapters': rendered.toc.length }">
    <aside v-if="rendered.toc.length" class="docs-sidebar">
      <button type="button" class="docs-directory-toggle" :aria-expanded="contentsOpen" @click="contentsOpen = !contentsOpen">
        <Icon name="book" size="sm" />
        <span>{{ t('publicDocs.contents') }}</span>
        <Icon :name="contentsOpen ? 'chevronUp' : 'chevronDown'" size="sm" class="ml-auto md:hidden" />
      </button>
      <nav :aria-label="t('publicDocs.contents')" class="docs-directory" :class="{ 'is-open': contentsOpen }">
        <a v-for="chapter in rendered.toc" :key="chapter.id" :href="`#${chapter.id}`"
          :class="{ active: activeChapter === chapter.id, nested: chapter.level > 2 }"
          :aria-current="activeChapter === chapter.id ? 'location' : undefined"
          @click="selectChapter(chapter.id)">{{ chapter.text }}</a>
      </nav>
    </aside>
    <article class="min-w-0">
      <header class="docs-article-header">
        <p class="text-xs font-semibold text-cyan-300">{{ t('publicDocs.eyebrow') }}</p>
        <h1 class="mt-3 break-words text-3xl font-semibold leading-snug text-white">{{ title || t('publicDocs.title') }}</h1>
      </header>
      <div v-if="content.trim()" ref="body" class="docs-markdown" @click="onContentClick" v-html="rendered.html" />
      <div v-else class="py-14" role="status">
        <Icon name="book" size="lg" class="text-cyan-300" />
        <h2 class="mt-5 text-xl font-semibold text-white">{{ t('publicDocs.emptyTitle') }}</h2>
        <p class="mt-3 text-sm leading-7 text-slate-400">{{ t('publicDocs.emptyDescription') }}</p>
        <RouterLink to="/model-plaza" class="mt-6 inline-flex items-center gap-2 text-sm font-semibold text-cyan-200">
          {{ t('publicDocs.modelPlaza') }}<Icon name="arrowRight" size="sm" />
        </RouterLink>
      </div>
      <p class="sr-only" role="status" aria-live="polite">{{ copyStatus }}</p>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { renderPublicDocsMarkdown } from '@/utils/publicDocsMarkdown'

const props = defineProps<{ title?: string; content: string; preview?: boolean }>()
const { t } = useI18n()
const rendered = computed(() => renderPublicDocsMarkdown(props.content, t('publicDocs.copyCode')))
const body = ref<HTMLElement>()
const contentsOpen = ref(false)
const activeChapter = ref('')
const copyStatus = ref('')
let observer: IntersectionObserver | undefined
let copyTimer: ReturnType<typeof setTimeout> | undefined

function selectChapter(id: string) {
  activeChapter.value = id
  contentsOpen.value = false
}

async function onContentClick(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target : null
  const button = target?.closest<HTMLButtonElement>('[data-docs-copy]')
  if (!button || !body.value?.contains(button)) return
  const code = button.closest('pre')?.querySelector('code')?.textContent || ''
  clearTimeout(copyTimer)
  body.value.querySelectorAll<HTMLButtonElement>('[data-docs-copy]').forEach(item => { item.textContent = t('publicDocs.copyCode') })
  try {
    await navigator.clipboard.writeText(code)
    copyStatus.value = t('publicDocs.copied')
  } catch {
    copyStatus.value = t('publicDocs.copyFailed')
  }
  button.textContent = copyStatus.value
  copyTimer = setTimeout(() => {
    button.textContent = t('publicDocs.copyCode')
    copyStatus.value = ''
  }, 2000)
}

watch(rendered, async (document) => {
  observer?.disconnect()
  activeChapter.value = document.toc[0]?.id || ''
  await nextTick()
  if (!body.value || props.preview) return
  observer = new IntersectionObserver(entries => {
    const current = entries.find(entry => entry.isIntersecting)
    if (current) activeChapter.value = current.target.id
  }, { rootMargin: '-100px 0px -65% 0px' })
  body.value.querySelectorAll('h1, h2, h3, h4, h5, h6').forEach(heading => observer?.observe(heading))
  try {
    const id = decodeURIComponent(window.location.hash.slice(1))
    if (id && document.toc.some(chapter => chapter.id === id)) {
      body.value.querySelectorAll<HTMLElement>('[id]').forEach(heading => {
        if (heading.id === id) heading.scrollIntoView()
      })
      activeChapter.value = id
    }
  } catch { /* A malformed URL fragment must not prevent reading documentation. */ }
}, { immediate: true })

onBeforeUnmount(() => {
  observer?.disconnect()
  clearTimeout(copyTimer)
})
</script>

<style scoped>
.public-docs-content { color: #cbd5e1; letter-spacing: 0; }
.docs-sidebar { min-width: 0; margin-bottom: 28px; }
.docs-directory-toggle { display: flex; width: 100%; align-items: center; gap: 10px; padding: 12px 0; color: #e2e8f0; font-size: 13px; font-weight: 600; }
.docs-directory { display: none; max-height: 55vh; overflow-y: auto; padding: 6px 0; }
.docs-directory.is-open { display: block; }
.docs-directory a { display: block; border-left: 1px solid #ffffff18; padding: 9px 14px; color: #94a3b8; font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.docs-directory a.nested { padding-left: 26px; }
.docs-directory a:hover, .docs-directory a.active { border-color: #67e8f9; color: #a5f3fc; background: #67e8f908; }
.docs-article-header { border-bottom: 1px solid #ffffff18; padding-bottom: 28px; margin-bottom: 28px; }
.docs-markdown { font-size: 15px; line-height: 1.85; overflow-wrap: anywhere; }
.docs-markdown :deep(h1), .docs-markdown :deep(h2), .docs-markdown :deep(h3), .docs-markdown :deep(h4), .docs-markdown :deep(h5), .docs-markdown :deep(h6) { color: #f8fafc; font-weight: 600; line-height: 1.5; margin: 32px 0 14px; scroll-margin-top: 108px; }
.docs-markdown :deep(h1) { font-size: 26px; }
.docs-markdown :deep(h2) { font-size: 22px; }
.docs-markdown :deep(h3) { font-size: 18px; }
.docs-markdown :deep(p), .docs-markdown :deep(ul), .docs-markdown :deep(ol) { margin-bottom: 18px; }
.docs-markdown :deep(ul) { list-style: disc; padding-left: 24px; }
.docs-markdown :deep(ol) { list-style: decimal; padding-left: 24px; }
.docs-markdown :deep(li) { margin: 6px 0; }
.docs-markdown :deep(a) { color: #67e8f9; text-decoration: underline; text-underline-offset: 4px; }
.docs-markdown :deep(strong) { color: #e2e8f0; font-weight: 600; }
.docs-markdown :deep(code) { font-size: 13px; background: #ffffff08; padding: 3px 5px; border-radius: 4px; color: #a7f3d0; }
.docs-markdown :deep(pre) { position: relative; overflow-x: auto; border: 1px solid #ffffff18; border-radius: 8px; background: #10191f; padding: 48px 20px 20px; margin: 24px 0; }
.docs-markdown :deep(pre code) { padding: 0; background: none; color: #d6e6ed; line-height: 1.85; white-space: pre; }
.docs-markdown :deep([data-docs-copy]) { position: absolute; right: 12px; top: 10px; border: 1px solid #ffffff20; border-radius: 4px; padding: 2px 10px; color: #a5f3fc; background: #10191f; font-family: sans-serif; font-size: 12px; }
.docs-markdown :deep([data-docs-copy]:hover) { border-color: #67e8f9; }
.docs-markdown :deep(blockquote) { border-left: 3px solid #fbbf24; background: #fbbf2408; padding: 14px 20px; margin: 24px 0; color: #cbd5e1; }
.docs-markdown :deep(blockquote p:last-child) { margin-bottom: 0; }
.docs-markdown :deep(table) { display: block; width: 100%; overflow-x: auto; border-collapse: collapse; margin: 24px 0; }
.docs-markdown :deep(th), .docs-markdown :deep(td) { border-bottom: 1px solid #ffffff18; padding: 12px 16px; text-align: left; font-size: 13px; min-width: 120px; }
.docs-markdown :deep(th) { background: #ffffff05; color: #e2e8f0; }
.docs-markdown :deep(img) { max-width: 100%; height: auto; border-radius: 8px; margin: 24px 0; }
.docs-markdown :deep(hr) { border-color: #ffffff18; margin: 32px 0; }
.public-docs-content :deep(:focus-visible) { outline: 2px solid #67e8f9; outline-offset: 4px; }
@media (min-width: 768px) {
  .has-chapters { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 48px; }
  .docs-sidebar { position: sticky; top: 104px; align-self: start; margin-bottom: 0; }
  .docs-directory { display: block; max-height: calc(100vh - 180px); }
  .docs-directory-toggle { pointer-events: none; }
}
</style>
