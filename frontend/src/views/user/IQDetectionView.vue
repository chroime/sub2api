<template>
  <AppLayout>
    <div class="iq-page mx-auto min-h-full max-w-screen-2xl space-y-8 px-4 py-6 sm:px-6 lg:px-8" data-test="iq-detection-page">
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 class="mt-2 text-2xl font-semibold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('iqDetection.title') }}</h1>
        <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-500 dark:text-dark-300">{{ t('iqDetection.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="loading" data-test="iq-refresh" @click="load">
        <Icon name="refresh" size="sm" />
        {{ loading ? t('common.loading') : t('common.refresh') }}
      </button>
    </header>

    <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-200">{{ error }}</p>
    <p v-if="loading" role="status" class="rounded-xl border border-gray-200 bg-white px-5 py-10 text-center text-sm text-gray-500 shadow-sm dark:border-dark-700 dark:bg-dark-800">{{ t('common.loading') }}</p>

    <template v-else>
      <section class="space-y-4 rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800" aria-labelledby="iq-timeline-title">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="iq-timeline-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('iqDetection.timelineTitle') }}</h2>
          </div>
          <div class="flex flex-wrap gap-3 text-xs text-gray-500 dark:text-dark-400"><span><i class="iq-dot bg-emerald-500" />{{ t('iqDetection.pass') }}</span><span><i class="iq-dot bg-rose-500" />{{ t('iqDetection.fail') }}</span></div>
        </div>
        <div class="overflow-x-auto" data-test="iq-timeline" :aria-label="t('iqDetection.timelineTitle')">
          <div class="flex min-w-[388px] gap-1">
            <span v-for="point in dashboard.timeline" :key="point.at" data-test="iq-timeline-point" class="h-8 min-w-1 flex-1 rounded-sm" :class="timelineClass(point.status)" :title="`${formatTime(point.at)} · ${timelineLabel(point.status)}`" />
          </div>
        </div>
        <div class="flex justify-between text-[11px] text-gray-400"><span>{{ t('iqDetection.hoursAgo', { hours: dashboard.window_hours }) }}</span><span>{{ t('iqDetection.now') }}</span></div>
      </section>

      <section class="space-y-4" aria-labelledby="iq-pelican-title">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="iq-pelican-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('iqDetection.pelicanTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.pelicanHint') }}</p>
          </div>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.recentCount', { count: dashboard.pelican_works.length }) }}</span>
        </div>
        <div v-if="dashboard.pelican_works.length" class="grid grid-cols-1 items-start gap-5 md:grid-cols-2 lg:grid-cols-4" data-test="pelican-work-grid">
          <article v-for="work in dashboard.pelican_works" :key="work.id" class="group flex h-[620px] min-w-0 flex-col overflow-hidden rounded-[1.35rem] border border-gray-200/90 bg-white shadow-sm ring-1 ring-black/[0.02] transition duration-300 hover:-translate-y-0.5 hover:shadow-lg dark:border-dark-700 dark:bg-dark-800 dark:ring-white/[0.03]" data-test="pelican-work-card">
            <div class="iq-pelican-preview relative h-[420px] shrink-0 overflow-hidden bg-slate-100 dark:bg-dark-900" data-test="pelican-work-preview">
              <iframe :key="`${work.id}:${replayNonce[work.id] || 0}`" :srcdoc="preview(work.html, work.id, 'card')" :title="`${t('iqDetection.pelicanTitle')} ${work.model}`" :data-pelican-preview-id="work.id" data-pelican-preview-surface="card" :style="previewFrameStyle(work.id)" data-test="pelican-work-preview-frame" sandbox="allow-scripts" scrolling="no" referrerpolicy="no-referrer" class="pointer-events-none absolute left-1/2 top-0 block w-full select-none border-0" />
              <div class="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-black/25 via-black/5 to-transparent" />
              <button type="button" class="absolute bottom-3 right-3 inline-flex items-center gap-1.5 rounded-full bg-white/95 px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-lg ring-1 ring-black/5 transition hover:bg-white dark:bg-dark-800/95 dark:text-dark-100 dark:ring-white/10 dark:hover:bg-dark-800" @click="replay(work.id)"><Icon name="play" size="xs" />{{ t('iqDetection.replay') }}</button>
            </div>
            <div class="flex h-[200px] flex-none flex-col gap-2 p-3 text-sm" data-test="pelican-work-meta">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="truncate font-semibold leading-5 text-gray-800 dark:text-dark-100" data-test="pelican-work-group">{{ work.group_name || work.account_label }}</p>
                  <time class="mt-1 block text-xs leading-4 text-gray-400" :datetime="work.created_at" data-test="pelican-work-time">{{ formatTime(work.created_at) }}</time>
                </div>
                <span data-test="pelican-work-status" class="shrink-0 rounded-full px-2.5 py-1 text-[11px] font-semibold" :class="work.status === 'succeeded' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'">{{ work.status === 'succeeded' ? t('iqDetection.pass') : t('iqDetection.fail') }}</span>
              </div>
              <div class="flex min-w-0 items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
                <span class="min-w-0 truncate font-medium text-gray-600 dark:text-dark-200">{{ work.model }}</span>
                <span class="shrink-0 rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-700">{{ effortLabel(work.effort) }}</span>
              </div>
              <div class="flex items-center justify-between gap-2 border-t border-gray-100 pt-2 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400" data-test="pelican-work-footer"><span class="inline-flex items-center gap-2"><span class="rounded-full bg-primary-50 px-2 py-0.5 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300" data-test="pelican-work-format">HTML</span><span>{{ t('iqDetection.generated') }}</span></span><span class="shrink-0">{{ work.duration_ms }} ms</span></div>
              <button type="button" class="inline-flex items-center gap-1.5 self-start text-xs font-semibold text-primary-700 transition hover:text-primary-800 hover:underline dark:text-primary-300 dark:hover:text-primary-200" data-test="pelican-work-view" @click="openWork(work)"><Icon name="externalLink" size="xs" />{{ t('iqDetection.viewWork') }}</button>
            </div>
          </article>
        </div>
        <div v-if="dashboard.pelican_works.length || pelicanPage > 1 || dashboard.pelican_has_more" class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm dark:border-dark-700 dark:bg-dark-800" data-test="pelican-pagination">
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.page', { page: pelicanPage }) }}</span>
          <div class="flex items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm" data-test="pelican-prev" :disabled="pelicanLoading || pelicanPage <= 1" @click="changePelicanPage(pelicanPage - 1)">{{ t('pagination.previous') }}</button>
            <button type="button" class="btn btn-secondary btn-sm" data-test="pelican-next" :disabled="pelicanLoading || !dashboard.pelican_has_more" @click="changePelicanPage(pelicanPage + 1)">{{ t('pagination.next') }}</button>
          </div>
        </div>
        <div v-else class="rounded-xl border border-dashed border-gray-300 bg-white px-5 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-800">{{ t('iqDetection.emptyPelican') }}</div>
      </section>
      <BaseDialog v-if="selectedWork" :show="true" :title="`${t('iqDetection.pelicanTitle')} · ${selectedWork.model}`" width="wide" @close="selectedWork = null">
        <div ref="dialogPreviewRef" class="relative aspect-[6/5] min-h-[24rem] overflow-hidden rounded-xl bg-slate-50 dark:bg-dark-900" data-test="pelican-work-dialog-preview">
          <iframe :srcdoc="preview(selectedWork.html, selectedWork.id, 'dialog')" :title="selectedWork.model" :data-pelican-preview-id="selectedWork.id" data-pelican-preview-surface="dialog" :style="previewDialogFrameStyle(selectedWork.id)" data-test="pelican-work-dialog-frame" sandbox="allow-scripts" scrolling="no" referrerpolicy="no-referrer" class="pointer-events-none absolute left-1/2 top-0 block select-none border-0" />
        </div>
      </BaseDialog>
    </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import userAPI, { type IQDashboard } from '@/api/user'
import { modelPreviewDocument, modelPreviewCSP } from '@/views/admin/upstream-governance/model-preview'

const { t } = useI18n()
type PreviewSurface = 'card' | 'dialog'
type PreviewMetric = { width: number; height: number }
const loading = ref(true)
const error = ref('')
const dashboard = ref<IQDashboard>({ pelican_works: [], pelican_page: 1, pelican_page_size: 8, pelican_has_more: false, timeline: [], window_hours: 24, generated_at: '' })
const replayNonce = ref<Record<string, number>>({})
const previewMetrics = ref<Record<string, Partial<Record<PreviewSurface, PreviewMetric>>>>({})
const pelicanLoading = ref(false)
const selectedWork = ref<IQDashboard['pelican_works'][number] | null>(null)
const dialogPreviewRef = ref<HTMLElement | null>(null)
const dialogPreviewViewport = ref({ width: 0, height: 0 })
let dialogPreviewObserver: ResizeObserver | null = null
const pelicanPage = computed(() => dashboard.value.pelican_page || 1)
const pelicanPreviewHeight = 420
const maxPelicanContentHeight = 2400
const pelicanPreviewStyle = '<style data-pelican-preview-style>html,body{width:100%!important;min-height:0!important;height:auto!important;max-height:none!important;margin:0!important;overflow:hidden!important;overscroll-behavior:none!important;scrollbar-width:none!important}html::-webkit-scrollbar,body::-webkit-scrollbar{display:none!important;width:0!important;height:0!important}*,*::before,*::after{box-sizing:border-box!important}body>*{max-width:100%!important;min-height:0!important;height:auto!important;max-height:none!important}svg,canvas,img,video{max-width:100%!important;max-height:none!important}</style>'

function pelicanPreviewScript(id: string, surface: PreviewSurface) {
  const serializedID = JSON.stringify(id)
  const serializedSurface = JSON.stringify(surface)
  return `<script data-pelican-preview-size>(function(){const id=${serializedID},surface=${serializedSurface};let raf=0,last='';const send=function(){raf=0;const root=document.documentElement,body=document.body;if(!root)return;const width=Math.max(root.scrollWidth||0,root.clientWidth||0,body?.scrollWidth||0);const height=Math.ceil(Math.max(root.scrollHeight||0,root.offsetHeight||0,body?.scrollHeight||0,body?.offsetHeight||0));if(!width||!height)return;const key=width+':'+height;if(key===last)return;last=key;window.parent.postMessage({type:'pelican-preview-size',id,surface,width,height},'*')};const schedule=function(){if(!raf)raf=window.requestAnimationFrame(send)};const start=function(){schedule();if(window.ResizeObserver){const observer=new ResizeObserver(schedule);observer.observe(document.documentElement);if(document.body)observer.observe(document.body)}if(window.MutationObserver){new MutationObserver(schedule).observe(document.documentElement,{childList:true,subtree:true})}window.addEventListener('load',schedule,{once:true});window.setTimeout(schedule,80);window.setTimeout(schedule,300)};if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start()})();<\/script>`
}

async function fetchDashboard(page = 1) {
  const initialLoad = page === 1 && !dashboard.value.generated_at
  if (initialLoad) loading.value = true
  pelicanLoading.value = true
  error.value = ''
  try {
    dashboard.value = await userAPI.getIQDetection({
      hours: 24,
      limit: 8,
      ...(page > 1 ? { pelican_page: page, pelican_page_size: 8 } : {})
    })
  } catch {
    error.value = t('iqDetection.loadFailed')
  } finally {
    pelicanLoading.value = false
    if (initialLoad) loading.value = false
  }
}
function load() { void fetchDashboard(1) }
function changePelicanPage(page: number) { if (page >= 1 && !pelicanLoading.value) void fetchDashboard(page) }
function formatTime(value: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '—'
  const parts = new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23', timeZone: 'Asia/Shanghai' }).formatToParts(date)
  const values = Object.fromEntries(parts.map(({ type, value: part }) => [type, part]))
  return `${values.year}-${values.month}-${values.day} ${values.hour}:${values.minute}:${values.second}`
}
function effortLabel(value: string) { return value?.trim() || '—' }
function timelineClass(status: string) { return status === 'fail' ? 'bg-rose-500' : 'bg-emerald-500' }
function timelineLabel(status: string) { return status === 'fail' ? t('iqDetection.fail') : t('iqDetection.pass') }
function previewFrameStyle(id: string) {
  const measuredHeight = previewMetrics.value[id]?.card?.height
  if (!measuredHeight) return { height: '100%', transform: 'translateX(-50%) scale(1)', transformOrigin: 'top center' }
  const scale = Math.min(1, pelicanPreviewHeight / measuredHeight)
  return { height: `${measuredHeight}px`, transform: `translateX(-50%) scale(${scale})`, transformOrigin: 'top center' }
}
function previewDialogFrameStyle(id: string) {
  const metric = previewMetrics.value[id]?.dialog
  const viewport = dialogPreviewViewport.value
  if (!metric || !viewport.width || !viewport.height) {
    return { height: '100%', width: '100%', transform: 'translateX(-50%) scale(1)', transformOrigin: 'top center' }
  }
  const scale = Math.min(viewport.width / metric.width, viewport.height / metric.height)
  return {
    width: `${metric.width}px`,
    height: `${metric.height}px`,
    transform: `translateX(-50%) scale(${scale})`,
    transformOrigin: 'top center',
  }
}
function handlePreviewMessage(event: MessageEvent) {
  const data = event.data
  if (!data || data.type !== 'pelican-preview-size' || typeof data.id !== 'string') return
  if (!dashboard.value.pelican_works.some((work) => work.id === data.id)) return
  const frames = Array.from(document.querySelectorAll<HTMLIFrameElement>('iframe[data-pelican-preview-id]'))
  const sourceFrame = frames.find((frame) => frame.dataset.pelicanPreviewId === data.id && frame.contentWindow === event.source)
  if (frames.length && event.source != null && !sourceFrame) return
  const surface: PreviewSurface = data.surface === 'dialog' ? 'dialog' : 'card'
  const measuredWidth = Number(data.width)
  const measuredHeight = Number(data.height)
  if (!Number.isFinite(measuredWidth) || measuredWidth <= 0 || !Number.isFinite(measuredHeight) || measuredHeight <= 0) return
  const width = Math.ceil(measuredWidth)
  const height = Math.min(maxPelicanContentHeight, Math.ceil(measuredHeight))
  const current = previewMetrics.value[data.id]?.[surface]
  if (current?.width !== width || current?.height !== height) {
    previewMetrics.value = {
      ...previewMetrics.value,
      [data.id]: { ...previewMetrics.value[data.id], [surface]: { width, height } },
    }
  }
}
function preview(html: string, id?: string, surface: PreviewSurface = 'card') {
  const source = html || ''
  const instrumentation = id ? pelicanPreviewScript(id, surface) : ''
  const styled = /<\/head\s*>/i.test(source)
    ? source.replace(/<\/head\s*>/i, `${pelicanPreviewStyle}${instrumentation}</head>`)
    : /<html(?:\s[^>]*)?>/i.test(source)
      ? source.replace(/<html(?:\s[^>]*)?>/i, (tag) => `${tag}<head>${pelicanPreviewStyle}${instrumentation}</head>`)
      : `${pelicanPreviewStyle}${instrumentation}${source}`
  return `${modelPreviewDocument(styled)}<!-- ${modelPreviewCSP} -->`
}
function replay(id: string) { replayNonce.value[id] = (replayNonce.value[id] || 0) + 1 }
function openWork(work: IQDashboard['pelican_works'][number]) { selectedWork.value = work }
function stopDialogPreviewObserver() {
  dialogPreviewObserver?.disconnect()
  dialogPreviewObserver = null
}
function updateDialogPreviewViewport() {
  const element = dialogPreviewRef.value
  if (!element) return
  const rect = element.getBoundingClientRect()
  dialogPreviewViewport.value = { width: rect.width, height: rect.height }
}
watch(selectedWork, async (work) => {
  stopDialogPreviewObserver()
  dialogPreviewViewport.value = { width: 0, height: 0 }
  if (!work) return
  await nextTick()
  updateDialogPreviewViewport()
  if (typeof ResizeObserver === 'undefined' || !dialogPreviewRef.value) return
  dialogPreviewObserver = new ResizeObserver(updateDialogPreviewViewport)
  dialogPreviewObserver.observe(dialogPreviewRef.value)
})
onMounted(() => {
  window.addEventListener('message', handlePreviewMessage)
  load()
})
onBeforeUnmount(() => {
  stopDialogPreviewObserver()
  window.removeEventListener('message', handlePreviewMessage)
})
</script>

<style scoped>
.iq-page { background: linear-gradient(180deg, rgba(239, 246, 243, .7), transparent 18rem); }
.iq-dot { display: inline-block; width: .55rem; height: .55rem; border-radius: 9999px; margin-right: .35rem; vertical-align: .02rem; }
.iq-pelican-preview { touch-action: pan-y; }
</style>
