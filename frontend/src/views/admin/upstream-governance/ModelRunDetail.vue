<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import api, { type ModelReview, type ModelRun } from '@/api/admin/upstream-model-monitoring'
import { formatGovernanceTime } from './format'
import { errorKey } from './feedback'
import { downloadModelArtifact, modelHTMLArtifacts, modelPreviewCSP, modelPreviewDocument } from './model-preview'

const props = defineProps<{ siteId: number; runId: string; disabled?: boolean }>()
const emit = defineEmits<{ close: []; reviewed: [run: ModelRun] }>()
const { t } = useI18n()
const mt = (key: string) => t(`governance.modelMonitoring.${key}`)
const run = ref<ModelRun | null>(null), loading = ref(false), saving = ref(false), error = ref('')
const review = ref<ModelReview>('pending'), note = ref(''), section = ref('answer')
const artifactIndex = ref(0), previewOpen = ref(false), previewRevision = ref(0)
let generation = 0
const result = computed(() => run.value?.result)
const artifacts = computed(() => modelHTMLArtifacts(result.value?.response_text || '', result.value?.html || ''))
const artifact = computed(() => artifacts.value[artifactIndex.value])
const artifactOptions = computed(() => artifacts.value.map((item, value) => ({ value, label: item.source.startsWith('block:') ? `${mt('htmlBlock')} ${item.source.split(':')[1]}` : mt(item.source === 'original' ? 'originalHTML' : 'savedHTML') })))
const preview = computed(() => artifact.value ? modelPreviewDocument(artifact.value.html) : '')
const reviewOptions = computed(() => (['pending', 'pass', 'fail'] as const).map(value => ({ value, label: mt(`review_${value}`) })))
const unknown = (value: unknown) => value === null || value === undefined || value === '' ? mt('unknown') : String(value)
const percent = (value: number | null | undefined) => value == null ? mt('unknown') : `${value.toFixed(2)}%`
const pretty = (value: unknown) => value == null ? mt('unknown') : typeof value === 'string' ? value : JSON.stringify(value, null, 2)
const stateKeys = new Set(['usage_missing', 'tokenizer_unknown', 'estimate_only', 'within_tolerance', 'suspected_difference', 'incomparable_reasoning', 'invalid_usage', 'cache_incomparable', 'supported_parameter', 'unverified_parameter', 'unsupported', 'numeric_correct', 'numeric_incorrect', 'needs_review'])
const stateLabel = (value: string | undefined) => value ? stateKeys.has(value) ? mt(value) : value : mt('unknown')
async function load() {
  const request = ++generation
  run.value = null; error.value = ''; loading.value = true; saving.value = false
  previewOpen.value = false; artifactIndex.value = 0; section.value = 'answer'
  try {
    const value = await api.run(props.siteId, props.runId)
    if (request !== generation) return
    run.value = value; review.value = value.review || 'pending'; note.value = value.review_note || ''
  } catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) loading.value = false }
}
watch(() => [props.siteId, props.runId], load, { immediate: true })
watch(artifactIndex, () => { previewOpen.value = false; previewRevision.value++ })
onUnmounted(() => { generation++ })
async function saveReview() {
  if (saving.value || props.disabled || !run.value || !['pass', 'fail', 'pending'].includes(review.value)) return
  const request = generation
  saving.value = true; error.value = ''
  try {
    const value = await api.review(props.siteId, run.value.id, review.value, note.value)
    if (request !== generation) return
    run.value = value; emit('reviewed', value)
  } catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) saving.value = false }
}
function changeReview(value: unknown) {
  if (!saving.value && !props.disabled && (value === 'pending' || value === 'pass' || value === 'fail')) review.value = value
}
function changeArtifact(value: unknown) {
  if (typeof value === 'number' && artifacts.value[value]) artifactIndex.value = value
}
function download(kind: 'raw' | 'html') {
  if (!run.value) return
  const contents = kind === 'raw' ? result.value?.response_text : artifact.value?.html
  if (contents != null) downloadModelArtifact(contents, `model-${run.value.id.replace(/[^a-zA-Z0-9-]/g, '')}.${kind === 'raw' ? 'txt' : 'html'}`)
}
</script>

<template>
  <BaseDialog :show="true" :title="mt('runDetail')" width="extra-wide" :show-close-button="!saving" :close-on-escape="!saving" @close="emit('close')">
    <div class="min-w-0 space-y-5" data-test="model-run-detail">
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <p v-if="loading" role="status" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</p>
      <button v-if="!loading && !run" type="button" class="btn btn-secondary" @click="load">{{ t('common.refresh') }}</button>
      <template v-if="run">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0"><p class="break-all font-semibold">{{ run.request.config.model }}</p><p class="mt-1 text-xs text-gray-500">{{ mt(`template_${run.request.template}`) }} · {{ run.request.effort }} · {{ run.request.config.api_mode }}</p><time class="mt-1 block text-xs tabular-nums text-gray-500">{{ formatGovernanceTime(run.created_at) }}</time></div>
          <button type="button" class="btn btn-secondary text-xs" :disabled="loading || saving" @click="load">{{ t('common.refresh') }}</button>
        </div>
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('runState') }}</p><p class="mt-1 break-all text-sm font-semibold">{{ run.status }}</p><p v-if="result?.error_code" class="mt-1 break-all text-xs text-red-600">{{ result.error_code }}</p></div>
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('ttft') }}</p><p class="mt-1 text-sm font-semibold" data-test="model-ttft">{{ unknown(result?.ttft_ms) }}<span v-if="result?.ttft_ms != null"> ms</span></p></div>
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('duration') }}</p><p class="mt-1 text-sm font-semibold">{{ unknown(result?.duration_ms) }}<span v-if="result?.duration_ms != null"> ms</span></p></div>
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('effortSupport') }}</p><p class="mt-1 text-sm font-semibold">{{ stateLabel(result?.effort_support) }}</p></div>
        </div>
        <p class="text-xs leading-relaxed text-gray-500">{{ mt('timingHint') }}</p>
        <nav class="flex flex-wrap gap-2 border-b border-gray-100 pb-2 dark:border-dark-700" :aria-label="mt('runDetail')"><button v-for="value in ['answer', 'tokens', 'request']" :key="value" type="button" class="rounded-lg px-3 py-2 text-sm" :class="section === value ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" :aria-pressed="section === value" @click="section = value">{{ mt(value) }}</button></nav>
        <section v-if="section === 'answer'" class="min-w-0 space-y-4">
          <div v-if="run.request.template === 'candy'" class="space-y-2 rounded-xl border border-primary-100 bg-primary-50/50 p-4 text-sm dark:border-primary-900 dark:bg-primary-900/10" data-test="model-candy-reference">
            <p class="font-semibold">{{ mt('candyReference') }}</p><p>{{ mt('candyProof') }}</p><p class="text-xs leading-relaxed text-gray-500">{{ mt('candyBoundary') }}</p>
            <p>{{ mt('numericVerdict') }}：{{ stateLabel(result?.candy_verdict) }}<span v-if="result?.candy_answer != null"> · {{ result.candy_answer }}</span></p>
          </div>
          <template v-if="run.request.template === 'pelican'">
            <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
              <div class="flex flex-wrap items-center justify-between gap-3"><h4 class="font-medium">{{ mt('animationPreview') }}</h4><div class="flex flex-wrap gap-2"><button v-if="artifact" type="button" class="btn btn-primary text-xs" data-test="model-preview-play" @click="previewOpen = true; previewRevision++">{{ previewOpen ? mt('replay') : mt('playPreview') }}</button><button v-if="artifact" type="button" class="btn btn-secondary text-xs" data-test="model-download-html" @click="download('html')">{{ mt('downloadHTML') }}</button></div></div>
              <p class="mt-2 text-xs leading-relaxed text-gray-500">{{ mt('previewIsolation') }}</p>
              <Select v-if="artifacts.length" class="mt-3" :model-value="artifactIndex" :options="artifactOptions" :aria-label="mt('artifactSource')" @update:model-value="changeArtifact" />
              <p v-else class="mt-3 text-sm text-gray-500">{{ mt('noHTML') }}</p>
              <iframe v-if="previewOpen && artifact" :key="`${run.id}:${artifactIndex}:${previewRevision}`" data-test="model-artifact-frame" :title="mt('animationPreview')" sandbox="allow-scripts" :csp="modelPreviewCSP" referrerpolicy="no-referrer" :srcdoc="preview" class="mt-4 h-[420px] w-full rounded-lg border border-gray-200 bg-white dark:border-dark-600" />
            </div>
          </template>
          <div class="flex flex-wrap items-center justify-between gap-2"><h4 class="font-medium">{{ mt('originalResponse') }}</h4><button v-if="result?.response_text != null" type="button" class="btn btn-secondary text-xs" data-test="model-download-original" @click="download('raw')">{{ mt('downloadOriginal') }}</button></div>
          <pre data-test="model-original-response" class="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-900">{{ result?.response_text || mt('noResponse') }}</pre>
          <section v-if="result" class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
            <h4 class="font-medium">{{ mt('manualReview') }}</h4><p class="text-xs leading-relaxed text-gray-500">{{ run.request.template === 'pelican' ? mt('pelicanReviewHint') : mt('reviewHint') }}</p>
            <div class="grid gap-3 sm:grid-cols-[180px_1fr]"><Select data-test="model-review" :model-value="review" :options="reviewOptions" :disabled="saving || disabled" :aria-label="mt('manualReview')" @update:model-value="changeReview" /><textarea v-model="note" data-test="model-review-note" class="input min-h-20 w-full text-sm" :aria-label="mt('reviewNote')" :placeholder="mt('reviewNote')" :disabled="saving || disabled" maxlength="2000" /></div>
            <p v-if="run.reviewed_at" class="text-xs text-gray-500">{{ mt('reviewedAt') }} {{ formatGovernanceTime(run.reviewed_at) }} · {{ mt('reviewer') }} #{{ run.reviewer_id }}</p>
            <button type="button" data-test="model-review-save" class="btn btn-primary text-sm" :disabled="saving || disabled" @click="saveReview">{{ saving ? t('common.loading') : mt('saveReview') }}</button>
          </section>
        </section>
        <section v-if="section === 'tokens'" class="min-w-0 space-y-4" data-test="model-token-audit">
          <p class="rounded-xl bg-amber-50 p-3 text-xs leading-relaxed text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ mt('tokenBoundary') }}</p>
          <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="p-2">{{ mt('measurement') }}</th><th class="p-2">{{ mt('declared') }}</th><th class="p-2">{{ mt('localVisible') }}</th><th class="p-2">{{ mt('difference') }}</th><th class="p-2">{{ mt('comparison') }}</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr><th class="p-2 font-medium">{{ mt('inputTokens') }}</th><td class="p-2" data-test="declared-input">{{ unknown(result?.usage?.input_tokens) }}</td><td class="p-2">{{ unknown(result?.tokens?.input_local) }}</td><td class="p-2">{{ percent(result?.tokens?.input_delta_percent) }}</td><td class="p-2">{{ stateLabel(result?.tokens?.input_state) }}</td></tr><tr><th class="p-2 font-medium">{{ mt('outputTokens') }}</th><td class="p-2" data-test="declared-output">{{ unknown(result?.usage?.output_tokens) }}</td><td class="p-2">{{ unknown(result?.tokens?.output_local) }}</td><td class="p-2">{{ percent(result?.tokens?.output_delta_percent) }}</td><td class="p-2">{{ stateLabel(result?.tokens?.output_state) }}</td></tr></tbody></table></div>
          <dl class="grid gap-3 rounded-xl bg-gray-50 p-4 text-xs dark:bg-dark-900 sm:grid-cols-2"><div><dt class="text-gray-500">{{ mt('cachedTokens') }}</dt><dd class="mt-1">{{ unknown(result?.usage?.cached_tokens) }}</dd></div><div><dt class="text-gray-500">{{ mt('reasoningTokens') }}</dt><dd class="mt-1">{{ unknown(result?.usage?.reasoning_tokens) }}</dd></div><div><dt class="text-gray-500">{{ mt('cacheCreationTokens') }}</dt><dd class="mt-1">{{ unknown(result?.usage?.cache_creation_tokens) }}</dd></div><div><dt class="text-gray-500">{{ mt('declaredVisible') }}</dt><dd class="mt-1">{{ unknown(result?.tokens?.output_declared_visible) }}</dd></div><div><dt class="text-gray-500">{{ mt('declaredInputTotal') }}</dt><dd class="mt-1">{{ unknown(result?.tokens?.input_declared_total) }}</dd></div><div><dt class="text-gray-500">{{ mt('inputOverhead') }}</dt><dd class="mt-1">{{ unknown(result?.tokens?.input_overhead_margin) }}</dd></div><div><dt class="text-gray-500">Tokenizer</dt><dd class="mt-1 break-all">{{ unknown(result?.tokens?.tokenizer) }} · {{ unknown(result?.tokens?.tokenizer_version) }}</dd></div><div><dt class="text-gray-500">{{ mt('countSource') }}</dt><dd class="mt-1 break-all">{{ unknown(result?.tokens?.source) }}</dd></div><div><dt class="text-gray-500">{{ mt('inputSize') }}</dt><dd class="mt-1">{{ unknown(result?.tokens?.input_chars) }} {{ mt('characters') }} / {{ unknown(result?.tokens?.input_bytes) }} bytes</dd></div><div><dt class="text-gray-500">{{ mt('outputSize') }}</dt><dd class="mt-1">{{ unknown(result?.tokens?.output_chars) }} {{ mt('characters') }} / {{ unknown(result?.tokens?.output_bytes) }} bytes</dd></div></dl>
          <div class="space-y-2 text-xs"><p class="break-all">{{ mt('inputHash') }}：{{ unknown(result?.tokens?.input_hash) }}</p><p class="break-all">{{ mt('outputHash') }}：{{ unknown(result?.tokens?.output_hash) }}</p><p v-if="result?.tokens?.note" class="leading-relaxed text-gray-500">{{ result.tokens.note }}</p></div>
          <div v-if="result?.tokens?.markers?.length" class="space-y-2 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-600"><h4 class="font-medium">{{ mt('contextResults') }}</h4><p class="text-xs text-gray-500">{{ mt('contextBoundary') }}</p><p v-for="marker in result.tokens.markers" :key="marker.label" class="break-all">{{ marker.position }} · {{ marker.label }}：{{ mt(marker.found ? 'markerFound' : 'markerMissing') }}</p></div>
          <p v-if="result?.tokens?.echo_passed != null" class="text-sm">{{ mt('echoCheck') }}：{{ mt(result.tokens.echo_passed ? 'markerFound' : 'markerMissing') }}</p>
          <details><summary class="cursor-pointer text-sm font-medium">{{ mt('rawUsage') }}</summary><pre class="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ pretty(result?.raw_usage) }}</pre></details>
        </section>
        <section v-if="section === 'request'" class="space-y-4">
          <p class="text-xs text-gray-500">{{ mt('requestSnapshotHint') }}</p>
          <p class="text-sm">{{ mt('reportedModel') }}：{{ unknown(result?.response_model) }} · {{ mt('finishReason') }}：{{ unknown(result?.finish_reason) }}</p>
          <h4 class="text-sm font-medium">{{ mt('sentParameters') }}</h4><pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-gray-50 p-4 text-xs dark:bg-dark-900">{{ pretty(result?.sent_parameters) }}</pre>
          <details open><summary class="cursor-pointer text-sm font-medium">{{ mt('inputText') }}</summary><pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-gray-50 p-4 text-xs dark:bg-dark-900">{{ result?.input_text || mt('unknown') }}</pre></details>
          <details><summary class="cursor-pointer text-sm font-medium">{{ mt('rawRequest') }}</summary><pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-gray-50 p-4 text-xs dark:bg-dark-900">{{ pretty(result?.request_body) }}</pre></details>
        </section>
      </template>
    </div>
  </BaseDialog>
</template>
