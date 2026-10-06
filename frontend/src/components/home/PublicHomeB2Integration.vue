<template>
  <section class="integration" aria-labelledby="integration-title">
    <div class="integration-copy">
      <h2 id="integration-title">{{ t('home.public.b2.integrationTitle') }}</h2>
      <p>{{ t('home.public.b2.integrationDescription') }}</p>
      <a class="text-link" :href="props.docsHref">
        {{ t('home.public.b2.fullDocs') }}
        <Icon class="icon" name="arrowRight" size="sm" aria-hidden="true" />
      </a>
    </div>
    <div class="code-tool">
      <div class="code-toolbar">
        <div ref="tabList" class="code-tabs" role="tablist" :aria-label="t('home.public.b2.codeLanguage')">
          <button
            v-for="(tab, index) in tabs"
            :id="`tab-${tab.id}`"
            :key="tab.id"
            type="button"
            class="code-tab"
            role="tab"
            :data-code="tab.id"
            :aria-selected="activeId === tab.id"
            aria-controls="code-panel"
            :tabindex="activeId === tab.id ? 0 : -1"
            @click="selectCode(tab.id)"
            @keydown="navigateTabs($event, index)"
          >{{ tab.label }}</button>
        </div>
        <button
          id="copy-code"
          ref="copyButton"
          type="button"
          class="icon-button"
          :title="copyLabel"
          :aria-label="copyLabel"
          :aria-busy="copying"
          :disabled="copying"
          @click="copyCode"
        >
          <Icon class="icon" :name="copyState === 'success' ? 'check' : copyState === 'failure' ? 'exclamationCircle' : 'copy'" size="sm" aria-hidden="true" />
        </button>
      </div>
      <div id="code-panel" ref="codePanel" class="code-viewport" role="tabpanel" :aria-labelledby="`tab-${activeId}`" tabindex="0">
        <pre id="code-output" ref="codeOutput" :style="{ transform: `scale(${codeScale})`, transformOrigin: 'top left' }">{{ activeCode }}</pre>
      </div>
      <span class="sr-only" role="status" aria-live="polite">{{ copyState === 'idle' ? '' : copyLabel }}</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{ baseUrl: string; model: string; docsHref: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const tabs = [
  { id: 'curl', label: 'cURL' },
  { id: 'javascript', label: 'JavaScript' },
  { id: 'python', label: 'Python' },
] as const
type CodeId = typeof tabs[number]['id']
const activeId = ref<CodeId>('curl')
const tabList = ref<HTMLDivElement | null>(null)
const codePanel = ref<HTMLDivElement | null>(null)
const codeOutput = ref<HTMLPreElement | null>(null)
const copyButton = ref<HTMLButtonElement | null>(null)
const codeScale = ref(1)
const copyState = ref<'idle' | 'success' | 'failure'>('idle')
const copying = ref(false)
let copyToken = 0
let copyTimer: ReturnType<typeof setTimeout> | undefined
let resizeObserver: ResizeObserver | undefined
let stopped = false

// POSIX single quoting prevents shell expansion, including inside JSON strings.
function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'"'"'`)}'`
}

const snippets = computed<Record<CodeId, string>>(() => {
  const base = JSON.stringify(props.baseUrl)
  const model = JSON.stringify(props.model)
  const body = [`{"model":${model},`, '  "messages":[', '    {"role":"user","content":"Hi"}', '  ]}'].join('\n')
  return {
    curl: [
      `BASE_URL=${shellQuote(props.baseUrl)}`,
      'curl "$BASE_URL/chat/completions" \\',
      '  -H "Authorization: Bearer $API_KEY" \\',
      '  -H "Content-Type: application/json" \\',
      `  -d ${shellQuote(body)}`,
    ].join('\n'),
    javascript: [
      'import OpenAI from "openai";',
      'const ai = new OpenAI({',
      '  apiKey: process.env.API_KEY,',
      `  baseURL: ${base}`,
      '});',
      'await ai.chat.completions.create({',
      `  model: ${model},`,
      '  messages: [',
      '    { role: "user", content: "Hi" }',
      '  ]',
      '});',
    ].join('\n'),
    python: [
      'import os',
      'from openai import OpenAI',
      'ai = OpenAI(',
      '  api_key=os.environ["API_KEY"],',
      `  base_url=${base}`,
      ')',
      'ai.chat.completions.create(',
      `  model=${model},`,
      '  messages=[',
      '    {"role":"user","content":"Hi"}',
      '  ])',
    ].join('\n'),
  }
})
const activeCode = computed(() => snippets.value[activeId.value])
const copyLabel = computed(() => t(`home.public.b2.${copyState.value === 'success' ? 'copied' : copyState.value === 'failure' ? 'copyFailed' : 'copyCode'}`))

function resetCopy() {
  copyToken += 1
  clearTimeout(copyTimer)
  copyState.value = 'idle'
  copying.value = false
}

function selectCode(id: CodeId) {
  resetCopy()
  activeId.value = id
}

function navigateTabs(event: KeyboardEvent, index: number) {
  let next = index
  if (event.key === 'ArrowRight') next = (index + 1) % tabs.length
  else if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = tabs.length - 1
  else return
  event.preventDefault()
  const tab = tabs[next]!
  selectCode(tab.id)
  tabList.value?.querySelector<HTMLButtonElement>(`[data-code="${tab.id}"]`)?.focus({ preventScroll: true })
}

async function copyCode() {
  const token = ++copyToken
  clearTimeout(copyTimer)
  copying.value = true
  let success = false
  try { success = await copyToClipboard(activeCode.value, t('home.public.b2.copied')) } catch { success = false }
  if (stopped || token !== copyToken) return
  copying.value = false
  copyState.value = success ? 'success' : 'failure'
  copyTimer = setTimeout(resetCopy, 1800)
  await nextTick()
  if (!stopped && token === copyToken && document.activeElement === document.body) copyButton.value?.focus({ preventScroll: true })
}

function fitCode() {
  const panel = codePanel.value
  const output = codeOutput.value
  if (stopped || !panel || !output) return
  const style = getComputedStyle(panel)
  const padding = (value: string) => Number.parseFloat(value) || 0
  const width = panel.clientWidth - padding(style.paddingLeft) - padding(style.paddingRight)
  const height = panel.clientHeight - padding(style.paddingTop) - padding(style.paddingBottom)
  if (width <= 0 || height <= 0) return
  // Scroll extents are unscaled, so container resizes can also restore full size.
  const scale = Math.min(1, width / Math.max(1, output.scrollWidth), height / Math.max(1, output.scrollHeight))
  codeScale.value = scale
}

watch(activeCode, () => { resetCopy(); fitCode() }, { flush: 'post' })
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined' && codePanel.value) {
    resizeObserver = new ResizeObserver(fitCode)
    resizeObserver.observe(codePanel.value)
  }
  fitCode()
  document.fonts?.ready.then(fitCode)
})
onBeforeUnmount(() => {
  stopped = true
  resetCopy()
  resizeObserver?.disconnect()
})
</script>
