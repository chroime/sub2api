<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import api, { type BrowserAuthAction, type BrowserAuthJob, type Site } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'

const props = withDefaults(defineProps<{ show?: boolean; siteId: number; siteVersion: number; username: string; password: string }>(), { show: true })
const emit = defineEmits<{ close: []; connected: [site?: Site]; invalidated: [] }>()
const { t } = useI18n()
const job = ref<BrowserAuthJob | null>(null)
const visible = ref(false), completing = ref(false), error = ref(''), textInput = ref('')
const frameElement = ref<HTMLElement>()
const imageElement = ref<HTMLImageElement>()
const actualSize = ref(false), panMode = ref(false)
const state = computed(() => job.value?.status || (error.value ? 'failed' : 'starting'))
const interactive = computed(() => visible.value && job.value?.status === 'waiting' && !completing.value)
const frameSource = computed(() => {
  const frame = job.value?.frame
  return frame?.width === 1024 && frame.height === 720 && /^[A-Za-z0-9+/]+={0,2}$/.test(frame.image)
    ? `data:image/jpeg;base64,${frame.image}` : ''
})
let disposed = false, generation = 0
let operations = Promise.resolve()
let pollTimer: ReturnType<typeof setTimeout> | undefined
let moveTimer: ReturnType<typeof setTimeout> | undefined
let expiryTimer: ReturnType<typeof setTimeout> | undefined
let pendingMove: BrowserAuthAction | undefined
let queuedMove: { action: BrowserAuthAction } | undefined
let pointerId: number | undefined
let panStart: { pointerId: number; x: number; y: number; left: number; top: number } | undefined
let ownerSiteId = props.siteId
const cancelledJobs = new Set<string>()
const keys = new Set(['Enter', 'Tab', 'Escape', 'Backspace', 'Delete', 'ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'Space'])

function current(request: number) { return !disposed && visible.value && request === generation }
function clearTimers() {
  clearTimeout(pollTimer); clearTimeout(moveTimer); clearTimeout(expiryTimer)
  pollTimer = undefined; moveTimer = undefined; expiryTimer = undefined
  pendingMove = undefined; queuedMove = undefined; pointerId = undefined
  panStart = undefined
}
async function cancelRemote(siteId: number, jobId: string) {
  const key = `${siteId}:${jobId}`
  if (cancelledJobs.has(key)) return
  cancelledJobs.add(key)
  try { await api.cancelBrowserAuth(siteId, jobId) } catch { /* Jobs also expire server-side. */ }
}
function stop() {
  generation++
  visible.value = false
  clearTimers()
  textInput.value = ''
  const previous = job.value, siteId = ownerSiteId
  if (previous && previous.status !== 'completed') void cancelRemote(siteId, previous.id)
}
function close() { if (completing.value) return; stop(); emit('close') }
function fail(cause: unknown) {
  clearTimers()
  if (job.value) job.value = { ...job.value, status: 'failed', frame: undefined }
  error.value = t('governance.browserAuthFailed')
  if ((cause as { reason?: string })?.reason === 'stale_preview') emit('invalidated')
}
// Input, polling and completion share a queue; cancellation can interrupt a pending request.
function enqueue(operation: () => Promise<void>, request = generation) {
  operations = operations.then(async () => {
    if (!current(request)) return
    try { await operation() } catch (cause) { if (current(request)) fail(cause) }
  })
  return operations
}
function updateJob(next: BrowserAuthJob, request: number) {
  if (!current(request)) return
  if (next.site_id !== ownerSiteId || (job.value && next.id !== job.value.id)) throw new Error('invalid browser job')
  job.value = next
  const siteId = ownerSiteId
  clearTimeout(pollTimer); clearTimeout(expiryTimer)
  if (['starting', 'waiting', 'ready'].includes(next.status)) {
    const remaining = Date.parse(next.expires_at) - Date.now()
    if (!Number.isFinite(remaining)) throw new Error('invalid browser expiry')
    expiryTimer = setTimeout(() => {
      if (!current(request)) return
      generation++
      job.value = { ...next, status: 'expired', frame: undefined }
      completing.value = false
      clearTimers()
      void cancelRemote(siteId, next.id)
    }, Math.max(0, remaining))
  }
  if (next.status === 'starting' || next.status === 'waiting') {
    pollTimer = setTimeout(() => {
      void enqueue(async () => {
        if (!job.value || !['starting', 'waiting'].includes(job.value.status)) return
        updateJob(await api.browserAuth(siteId, next.id), request)
      }, request)
    }, 1000)
  } else {
    clearTimeout(moveTimer)
    pendingMove = undefined; queuedMove = undefined
  }
}
function start() {
  if (disposed) return
  const request = ++generation, siteId = props.siteId
  ownerSiteId = siteId
  const credentials = { expected_site_version: props.siteVersion, username: props.username.trim(), password: props.password }
  job.value = null; error.value = ''; completing.value = false; visible.value = true
  actualSize.value = false; panMode.value = false
  void enqueue(async () => {
    const created = await api.startBrowserAuth(siteId, credentials)
    if (!current(request)) { await cancelRemote(siteId, created.id); return }
    updateJob(created, request)
  }, request)
}
watch(() => props.show, show => { if (show) start(); else stop() }, { immediate: true })
watch(() => [props.siteId, props.siteVersion], () => { stop(); emit('close') })
onUnmounted(() => { stop(); disposed = true })

function coordinates(event: MouseEvent | PointerEvent | WheelEvent, requireInside = false) {
  const rect = (actualSize.value && imageElement.value ? imageElement.value : frameElement.value)?.getBoundingClientRect()
  if (!rect || rect.width <= 0 || rect.height <= 0 || !Number.isFinite(event.clientX) || !Number.isFinite(event.clientY)) return null
  const relativeX = event.clientX - rect.left, relativeY = event.clientY - rect.top
  if (requireInside && (relativeX < 0 || relativeY < 0 || relativeX >= rect.width || relativeY >= rect.height)) return null
  return {
    x: Math.max(0, Math.min(1023, Math.floor(relativeX * 1024 / rect.width))),
    y: Math.max(0, Math.min(719, Math.floor(relativeY * 720 / rect.height))),
  }
}
function queueAction(action: BrowserAuthAction) {
  if (!interactive.value) return
  const jobId = job.value!.id, siteId = ownerSiteId
  void enqueue(async () => {
    if (interactive.value) await api.browserAuthAction(siteId, jobId, action)
  })
}
function flushMove() {
  clearTimeout(moveTimer); moveTimer = undefined
  if (!pendingMove || !interactive.value) return
  if (queuedMove) queuedMove.action = pendingMove
  else {
    const next = { action: pendingMove }, jobId = job.value!.id, siteId = ownerSiteId
    queuedMove = next
    void enqueue(async () => {
      if (queuedMove === next) queuedMove = undefined
      if (interactive.value) await api.browserAuthAction(siteId, jobId, next.action)
    })
  }
  pendingMove = undefined
}
function action(action: BrowserAuthAction) { flushMove(); queuedMove = undefined; queueAction(action) }
function pointerDown(event: PointerEvent) {
  if (!interactive.value || event.button !== 0 || pointerId != null || panStart) return
  if (actualSize.value && panMode.value && frameElement.value) {
    event.preventDefault()
    panStart = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, left: frameElement.value.scrollLeft, top: frameElement.value.scrollTop }
    frameElement.value.setPointerCapture?.(event.pointerId)
    return
  }
  const point = coordinates(event, true)
  if (!point) return
  event.preventDefault()
  frameElement.value?.focus()
  pointerId = event.pointerId
  frameElement.value?.setPointerCapture?.(event.pointerId)
  action({ type: 'pointer_down', ...point })
}
function pointerMove(event: PointerEvent) {
  if (panStart) {
    if (event.pointerId === panStart.pointerId && frameElement.value) {
      frameElement.value.scrollLeft = panStart.left + panStart.x - event.clientX
      frameElement.value.scrollTop = panStart.top + panStart.y - event.clientY
    }
    return
  }
  if (actualSize.value && panMode.value) return
  if (!interactive.value || (pointerId != null && event.pointerId !== pointerId)) return
  const point = coordinates(event, pointerId == null)
  if (!point) return
  pendingMove = { type: 'pointer_move', ...point }
  if (!moveTimer) moveTimer = setTimeout(flushMove, 50)
}
function pointerUp(event: PointerEvent) {
  if (panStart) {
    if (event.pointerId !== panStart.pointerId) return
    panStart = undefined
    if (frameElement.value?.hasPointerCapture?.(event.pointerId)) frameElement.value.releasePointerCapture(event.pointerId)
    return
  }
  if (pointerId == null || pointerId !== event.pointerId) return
  const point = coordinates(event)
  if (point) action({ type: 'pointer_up', ...point })
  pointerId = undefined
  if (frameElement.value?.hasPointerCapture?.(event.pointerId)) frameElement.value.releasePointerCapture(event.pointerId)
}
function wheel(event: WheelEvent) {
  if (!interactive.value) return
  if (actualSize.value && panMode.value && frameElement.value) {
    event.preventDefault()
    frameElement.value.scrollLeft += event.deltaX
    frameElement.value.scrollTop += event.deltaY
    return
  }
  const point = coordinates(event, true)
  if (!point) return
  event.preventDefault()
  const scale = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 720 : 1
  const bound = (value: number) => Number.isFinite(value) ? Math.max(-2000, Math.min(2000, Math.round(value * scale))) : 0
  action({ type: 'wheel', ...point, delta_x: bound(event.deltaX), delta_y: bound(event.deltaY) })
}
function sendText(value: string) {
  if (!interactive.value || !value) return false
  if (new TextEncoder().encode(value).length > 2048) {
    error.value = t('governance.browserTextTooLong')
    return false
  }
  if ([...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) {
    error.value = t('governance.browserTextInvalid')
    return false
  }
  error.value = ''
  action({ type: 'text', text: value })
  return true
}
function sendTextInput() { if (sendText(textInput.value)) textInput.value = '' }
function keyDown(event: KeyboardEvent) {
  if (!interactive.value || event.ctrlKey || event.metaKey || event.altKey || event.isComposing) return
  const key = event.key === ' ' ? 'Space' : event.key
  if (keys.has(key)) { event.preventDefault(); event.stopPropagation(); action({ type: 'key', key }) }
  else if ([...event.key].length === 1) { event.preventDefault(); sendText(event.key) }
}
function paste(event: ClipboardEvent) {
  if (!interactive.value) return
  event.preventDefault()
  sendText(event.clipboardData?.getData('text/plain') || '')
}
function complete() {
  if (!visible.value || job.value?.status !== 'ready' || completing.value) return
  completing.value = true
  error.value = ''
  const request = generation, siteId = ownerSiteId, jobId = job.value.id
  void enqueue(async () => {
    try {
      const result = await api.completeBrowserAuth(siteId, jobId)
      if (!current(request)) return
      if (result.challenge) throw new Error('browser authorization incomplete')
      job.value = { ...job.value!, status: 'completed', frame: undefined }
      clearTimers()
      emit('connected', result.site)
    } catch (cause) {
      if ((cause as { reason?: string })?.reason === 'site_busy') {
        if (current(request)) error.value = t('governance.siteBusy')
      } else if ((cause as { status?: number })?.status === 0) {
        if (current(request)) error.value = t('governance.browserCompletionUncertain')
      } else throw cause
    } finally { if (current(request)) completing.value = false }
  }, request)
}
</script>

<template>
  <BaseDialog :show="show && visible" :title="t('governance.browserAuthorization')" width="extra-wide" :close-on-escape="false" :show-close-button="!completing" @close="close">
    <div class="space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
        <p role="status" :class="state === 'ready' ? 'text-green-700 dark:text-green-400' : 'text-gray-600 dark:text-gray-300'">{{ t(`governance.browserState_${state}`) }}</p>
        <p v-if="job" class="text-xs tabular-nums text-gray-500">{{ t('governance.expires') }} {{ formatGovernanceTime(job.expires_at) }}</p>
      </div>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <div v-if="frameSource" class="flex flex-wrap items-center gap-2 text-xs">
        <div role="group" :aria-label="t('governance.browserViewScale')" class="inline-flex overflow-hidden rounded border border-gray-200 dark:border-dark-600">
          <button type="button" data-test="browser-zoom-fit" :aria-pressed="!actualSize" :title="t('governance.browserFit')" class="px-3 py-1.5" :class="!actualSize ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" @click="actualSize = false; panMode = false">{{ t('governance.browserFit') }}</button>
          <button type="button" data-test="browser-zoom-actual" :aria-pressed="actualSize" :title="t('governance.browserActualSize')" class="border-l border-gray-200 px-3 py-1.5 dark:border-dark-600" :class="actualSize ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" @click="actualSize = true">1:1</button>
        </div>
        <button type="button" data-test="browser-pan" :aria-pressed="panMode" :disabled="!actualSize || !interactive" :title="t('governance.browserPan')" class="inline-flex items-center gap-1 rounded border border-gray-200 px-3 py-1.5 disabled:opacity-50 dark:border-dark-600" :class="panMode ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" @click="panMode = !panMode"><Icon name="arrowsUpDown" size="xs" />{{ t('governance.browserPan') }}</button>
      </div>
      <div ref="frameElement" data-test="browser-frame" role="application" :aria-label="t('governance.browserViewport')" :aria-disabled="!interactive" :tabindex="interactive ? 0 : -1" class="relative w-full rounded border border-gray-300 bg-gray-100 outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:bg-dark-900" :class="actualSize ? 'overflow-auto' : 'overflow-hidden'" style="aspect-ratio: 1024 / 720; touch-action: none" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerUp" @lostpointercapture="pointerUp" @wheel="wheel" @keydown="keyDown" @paste="paste" @contextmenu.prevent>
        <img v-if="frameSource" ref="imageElement" :src="frameSource" :alt="t('governance.browserViewport')" width="1024" height="720" draggable="false" class="pointer-events-none absolute left-0 top-0 select-none" :class="actualSize ? 'max-w-none' : 'h-full w-full'" :style="actualSize ? { width: '1024px', height: '720px' } : {}" />
        <div v-else class="absolute inset-0 flex items-center justify-center text-sm text-gray-500">{{ t(`governance.browserState_${state}`) }}</div>
      </div>
      <div class="flex min-w-0 items-center gap-2">
        <label for="governance-browser-text" class="sr-only">{{ t('governance.browserText') }}</label>
        <input id="governance-browser-text" v-model="textInput" data-test="browser-text" class="input min-w-0 flex-1" autocomplete="off" :placeholder="t('governance.browserText')" :disabled="!interactive" @keydown.enter.prevent="sendTextInput" />
        <button data-test="browser-send-text" type="button" class="btn btn-secondary h-10 w-10 shrink-0 p-2" :title="t('governance.browserSendText')" :aria-label="t('governance.browserSendText')" :disabled="!interactive || !textInput" @click="sendTextInput"><Icon name="arrowRight" size="sm" /></button>
      </div>
    </div>
    <template #footer>
      <button type="button" data-test="browser-cancel" class="btn btn-secondary" :disabled="completing" @click="close">{{ t('common.cancel') }}</button>
      <button type="button" data-test="browser-complete" class="btn btn-primary" :disabled="state !== 'ready' || completing" @click="complete"><Icon name="check" size="sm" class="mr-2" />{{ t(completing ? 'common.processing' : 'governance.browserComplete') }}</button>
    </template>
  </BaseDialog>
</template>
