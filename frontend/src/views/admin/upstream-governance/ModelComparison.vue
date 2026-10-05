<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import api, { type ModelEffort, type ModelRun, type ModelTestTemplate } from '@/api/admin/upstream-model-monitoring'
import { errorKey } from './feedback'
import { downloadModelArtifact, modelHTMLArtifacts, modelPreviewCSP, modelPreviewDocument } from './model-preview'

const props = defineProps<{ siteId: number; batchId: string; template: ModelTestTemplate; sample: number }>()
const emit = defineEmits<{ close: []; detail: [runId: string] }>()
const { t } = useI18n()
const mt = (key: string) => t(`governance.modelMonitoring.${key}`)
const runs = ref<ModelRun[]>([]), loading = ref(false), error = ref(''), playing = ref(false), revision = ref(0)
const selected = ref<Record<string, number>>({})
const efforts: ModelEffort[] = ['low', 'medium', 'high']
let generation = 0
const columns = computed(() => efforts.map(effort => {
  const run = runs.value.find(value => value.request.effort === effort)
  const artifacts = modelHTMLArtifacts(run?.result?.response_text || '', run?.result?.html || '')
  return { effort, run, artifacts, artifact: artifacts[selected.value[effort] || 0] }
}))
const count = (value: number | null | undefined) => value == null ? mt('unknown') : `${value} ms`
async function load() {
  const request = ++generation, id = props.siteId, batch = props.batchId
  loading.value = true; error.value = ''; runs.value = []; playing.value = false; selected.value = {}
  try {
    const first = await api.runs(id, 1, batch)
    const pages = Math.ceil(first.total / first.page_size)
    const rest = await Promise.all(Array.from({ length: Math.max(0, Math.min(pages, 15) - 1) }, (_, index) => api.runs(id, index + 2, batch)))
    if (request !== generation) return
    const candidates = [first, ...rest].flatMap(page => page.items).filter(run => run.batch_id === batch && run.request.template === props.template && run.request.sample === props.sample)
    const details = await Promise.all(candidates.map(run => api.run(id, run.id)))
    if (request === generation) runs.value = details
  } catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) loading.value = false }
}
watch(() => [props.siteId, props.batchId, props.template, props.sample], load, { immediate: true })
onUnmounted(() => { generation++ })
function choose(effort: string, value: unknown) { if (typeof value === 'number' && value >= 0) { selected.value[effort] = value; revision.value++ } }
function download(run: ModelRun, html?: string) { downloadModelArtifact(html ?? run.result?.response_text ?? '', `model-${run.id.replace(/[^a-zA-Z0-9-]/g, '')}.${html === undefined ? 'txt' : 'html'}`) }
</script>
<template>
  <BaseDialog :show="true" :title="mt('effortComparison')" width="full" @close="emit('close')">
    <section class="min-w-0 space-y-4" data-test="model-comparison">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><p class="font-semibold">{{ runs[0]?.request.config.model || mt(`template_${template}`) }}</p><p class="mt-1 text-xs text-gray-500">{{ mt(`template_${template}`) }} · {{ mt('sample') }} {{ sample }} · {{ mt('comparisonScope') }}</p></div><div class="flex gap-2"><button v-if="template === 'pelican'" type="button" class="btn btn-primary text-sm" data-test="model-comparison-play" :disabled="loading || !columns.some(column => column.artifact)" @click="playing = true; revision++">{{ mt(playing ? 'replay' : 'playPreview') }}</button><button type="button" class="btn btn-secondary text-sm" :disabled="loading" @click="load">{{ t('common.refresh') }}</button></div></div>
      <p v-if="template === 'pelican'" class="text-xs leading-relaxed text-gray-500">{{ mt('previewIsolation') }}</p><p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-else class="grid min-w-0 gap-4 lg:grid-cols-3"><article v-for="column in columns" :key="column.effort" class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-600" :data-test="`model-compare-${column.effort}`"><h4 class="font-semibold">{{ mt(`effort_${column.effort}`) }}</h4><template v-if="column.run"><p class="mt-2 text-xs text-gray-500">{{ column.run.status }} · TTFT {{ count(column.run.result?.ttft_ms) }} · {{ mt('duration') }} {{ count(column.run.result?.duration_ms) }}</p><p class="mt-2 text-xs text-gray-500">{{ mt('manualReview') }}：{{ mt(`review_${column.run.review || 'pending'}`) }}</p><p v-if="template === 'candy'" class="mt-3 text-sm">{{ mt('numericVerdict') }}：{{ mt(column.run.result?.candy_verdict || 'unknown') }}<span v-if="column.run.result?.candy_answer != null"> · {{ column.run.result.candy_answer }}</span></p><div v-if="template === 'pelican'" class="mt-3 space-y-3"><Select v-if="column.artifacts.length" :model-value="selected[column.effort] || 0" :options="column.artifacts.map((artifact, value) => ({ value, label: artifact.source.startsWith('block:') ? `${mt('htmlBlock')} ${artifact.source.split(':')[1]}` : mt(artifact.source === 'original' ? 'originalHTML' : 'savedHTML') }))" :aria-label="mt('artifactSource')" @update:model-value="choose(column.effort, $event)" /><iframe v-if="playing && column.artifact" :key="`${column.run.id}:${revision}:${selected[column.effort] || 0}`" :title="`${mt('animationPreview')} ${column.effort}`" sandbox="allow-scripts" :csp="modelPreviewCSP" referrerpolicy="no-referrer" :srcdoc="modelPreviewDocument(column.artifact.html)" class="h-80 w-full rounded-lg border border-gray-200 bg-white dark:border-dark-600" /><p v-if="!column.artifact" class="text-xs text-gray-500">{{ mt('noHTML') }}</p></div><details class="mt-4" :open="template !== 'pelican'"><summary class="cursor-pointer text-sm font-medium">{{ mt('originalResponse') }}</summary><pre class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ column.run.result?.response_text || mt('noResponse') }}</pre></details><div class="mt-4 flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-xs" @click="emit('detail', column.run.id)">{{ mt('details') }}</button><button v-if="column.run.result?.response_text" type="button" class="btn btn-secondary text-xs" @click="download(column.run)">{{ mt('downloadOriginal') }}</button><button v-if="column.artifact" type="button" class="btn btn-secondary text-xs" @click="download(column.run, column.artifact.html)">{{ mt('downloadHTML') }}</button></div></template><p v-else class="mt-4 text-sm text-gray-500">{{ mt('effortNotRun') }}</p></article></div>
    </section>
  </BaseDialog>
</template>
