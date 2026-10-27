<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Binding, ControlAction, Run } from '@/api/admin/account-quality'
import { dateTime, duration, sampleTone } from '@/views/admin/account-quality/quality'
import { chronologicalSamples, responsePreview, runTime } from './qualityPresentation'

const props = withDefaults(defineProps<{ bindings?: Binding[]; loading?: boolean; error?: boolean; disabled?: boolean }>(), { bindings: () => [] })
const emit = defineEmits<{ details: []; run: [Binding]; evidence: [Run]; control: [Binding, ControlAction] }>()
const { t, te } = useI18n()
const active = computed(() => props.bindings.find(item => item.isolated) || props.bindings.find(item => item.failures > 0) || props.bindings[0])
const samples = computed(() => chronologicalSamples(active.value?.recent_runs))
const latest = computed(() => samples.value.at(-1))
const latestTime = computed(() => latest.value ? runTime(latest.value) : active.value?.last_run_at || undefined)
const preview = computed(() => responsePreview(latest.value))
const networkTimeout = computed(() => latest.value?.error_class === 'network_timeout')
const latestTone = computed(() => sampleTone(latest.value || { execution_status: active.value?.execution_status || '', quality_verdict: active.value?.quality_verdict || '' }))
const automaticAllowed = computed(() => Boolean(active.value?.auto_control_eligible && active.value.control_mode === 'auto_quarantine'))
const automaticDisabled = computed(() => props.disabled || (!active.value?.auto_isolate && !automaticAllowed.value))
const automaticHint = computed(() => {
  if (active.value?.auto_isolate || automaticAllowed.value) return t('accountQuality.autoChangeHint')
  return t(active.value?.control_mode && active.value.control_mode !== 'auto_quarantine' ? 'accountQuality.autoModeRequired' : 'accountQuality.autoUnsupported')
})
const label = (kind: string, value?: string) => {
  const key = `accountQuality.${kind}_${kind === 'execution' && value === 'execution_error' ? 'error' : value || 'untested'}`
  return te(key) ? t(key) : value || '—'
}
const colors = { pass: 'bg-emerald-500', fail: 'bg-red-500', error: 'bg-amber-400', unknown: 'bg-gray-300 dark:bg-dark-500' }
const resultColors = {
  pass: 'border-emerald-300 text-emerald-700 dark:border-emerald-700 dark:text-emerald-300',
  fail: 'border-red-300 text-red-700 dark:border-red-800 dark:text-red-300',
  error: 'border-amber-300 text-amber-700 dark:border-amber-800 dark:text-amber-300',
  unknown: 'border-gray-200 text-gray-500 dark:border-dark-600 dark:text-dark-300',
}
const badge = computed(() => active.value?.isolated ? resultColors.fail : active.value?.failures ? resultColors.error : active.value?.state === 'healthy' ? resultColors.pass : latestTone.value === 'error' ? resultColors.error : resultColors.unknown)
const latestLabel = computed(() => {
  if (networkTimeout.value) return t('accountQuality.networkTimeout')
  const execution = latest.value?.execution_status || active.value?.execution_status
  const verdict = latest.value?.quality_verdict || active.value?.quality_verdict
  return execution === 'completed' ? label('verdict', verdict) : execution ? label('execution', execution) : t('accountQuality.state_untested')
})
const outputLabel = computed(() => preview.value || t(networkTimeout.value ? 'accountQuality.networkTimeoutHint' : 'accountQuality.noOutput'))
function requestControl(action: ControlAction) {
  if (!props.disabled && active.value && (action !== 'set_auto' || !automaticDisabled.value)) emit('control', active.value, action)
}
function sampleLabel(run: Run) {
  return [dateTime(runTime(run)), run.error_class === 'network_timeout' ? t('accountQuality.networkTimeout') : label('execution', run.execution_status), label('verdict', run.quality_verdict), `${run.template} ${run.template_version}`, run.model, run.reason || '—', run.id].join(' · ')
}
</script>

<template>
  <div class="quality-cell py-1.5 text-xs" data-test="account-quality-cell">
    <p v-if="loading && !active" class="text-gray-400">{{ t('accountQuality.loading') }}</p>
    <p v-else-if="error" class="text-amber-600">{{ t('accountQuality.loadFailed') }}</p>
    <template v-else>
      <div class="quality-cell-layout">
        <div class="min-w-0 space-y-2">
          <div class="flex min-h-7 items-center gap-2 border-b border-gray-200 pb-2 dark:border-dark-600">
            <button type="button" data-test="quality-state" class="inline-flex min-h-7 shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500" :class="badge" @click="emit('details')"><span v-if="active?.state === 'healthy'" aria-hidden="true">✓</span>{{ label('state', active?.state) }}</button>
            <button v-if="active" type="button" data-test="quality-manual-review" class="min-h-7 rounded border border-primary-200 px-2 py-1 font-medium text-primary-700 transition-colors hover:bg-primary-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:border-primary-800 dark:text-primary-300 dark:hover:bg-primary-900/20" :title="t('accountQuality.evidenceHint')" :disabled="disabled" @click="requestControl('confirm_false_positive')">{{ t('accountQuality.manualReview') }}</button>
            <button v-if="bindings.length > 1" type="button" class="text-gray-400 hover:text-primary-600" :title="t('accountQuality.detail')" @click="emit('details')">+{{ bindings.length - 1 }}</button>
          </div>
          <template v-if="active">
            <div class="flex items-center gap-2">
              <button type="button" role="switch" :aria-checked="active.auto_isolate" :aria-label="t('accountQuality.automatic')" :title="automaticHint" :disabled="automaticDisabled" class="relative inline-flex h-5 w-9 shrink-0 rounded-full border border-transparent transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500 disabled:cursor-not-allowed disabled:opacity-40" :class="active.auto_isolate ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-500'" @click="requestControl('set_auto')"><span aria-hidden="true" class="mt-0.5 block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform" :class="active.auto_isolate ? 'translate-x-[1.125rem]' : 'translate-x-0.5'" /></button>
              <span class="font-medium text-gray-600 dark:text-dark-200">{{ t('accountQuality.automatic') }}</span>
              <span class="ml-auto text-xs" :class="active.auto_isolate ? 'text-primary-600 dark:text-primary-400' : 'text-gray-400'">{{ t(active.auto_isolate ? 'accountQuality.on' : 'accountQuality.off') }}</span>
            </div>
            <p class="truncate text-[11px] leading-4 text-gray-400" :title="`${active.policy_name} · ${active.model} · ${t('accountQuality.failures')}: ${active.failures}`"><span>{{ active.interval_seconds ? t('accountQuality.checkingEvery', { interval: `${active.interval_seconds}${t('accountQuality.seconds')}` }) : t('accountQuality.policyDefaults') }}</span><span v-if="active.failure_threshold"> · {{ t('accountQuality.failureRule', { count: active.failure_threshold }) }}</span></p>
          </template>
          <button v-else type="button" class="text-gray-500 hover:text-primary-600 hover:underline" @click="emit('details')">{{ t('accountQuality.detail') }}</button>
        </div>
        <div v-if="active" data-test="quality-latest-result" class="min-w-0 space-y-1.5">
          <div class="flex min-h-7 items-center gap-2">
            <span data-test="quality-latest-verdict" class="shrink-0 rounded border px-1.5 py-1 text-[11px] font-medium leading-none" :class="resultColors[latestTone]">{{ latestLabel }}</span>
            <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-gray-400" :title="dateTime(latestTime)">{{ duration(latest ? latest.duration_ms : active.last_duration_ms) }}<template v-if="latestTime"> · <time :datetime="latestTime">{{ dateTime(latestTime) }}</time></template></span>
            <button type="button" data-test="quality-run-now" class="shrink-0 rounded py-1 font-medium text-primary-600 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:text-primary-400" :disabled="disabled" @click="emit('run', active)"><span aria-hidden="true">↻ </span>{{ t('accountQuality.runNow') }}</button>
          </div>
          <button type="button" data-test="quality-output-preview" class="block h-8 w-full truncate rounded border border-gray-200 px-2.5 text-left font-mono text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500 disabled:cursor-default dark:border-dark-600" :class="preview ? 'text-gray-700 hover:border-primary-300 dark:text-dark-100 dark:hover:border-primary-700' : 'text-gray-400'" :title="`${t('accountQuality.latestOutput')}: ${outputLabel}`" :aria-label="`${t('accountQuality.latestOutput')}: ${outputLabel}`" :disabled="!latest" @click="latest && emit('evidence', latest)">{{ outputLabel }}</button>
          <div data-test="quality-sample-row" class="flex h-3.5 flex-nowrap items-center gap-[3px]" :aria-label="t('accountQuality.recentSamplesCount', { count: samples.length })">
            <button v-for="sample in samples" :key="sample.id" data-test="quality-sample" :data-run-id="sample.id" type="button" class="h-3.5 min-w-0 max-w-2 flex-1 rounded-[3px] transition-opacity hover:opacity-70 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500" :class="colors[sampleTone(sample)]" :title="sampleLabel(sample)" :aria-label="sampleLabel(sample)" @click="emit('evidence', sample)" />
            <span v-if="!samples.length" class="text-[11px] text-gray-400">{{ t('accountQuality.noRuns') }}</span>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.quality-cell {
  width: 34rem;
  min-width: min(30rem, 100%);
  max-width: 100%;
  container-type: inline-size;
}
.quality-cell-layout {
  display: grid;
  grid-template-columns: 12rem minmax(0, 1fr);
  gap: 1.25rem;
}
@container (max-width: 29.999rem) {
  .quality-cell-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.75rem;
  }
}
</style>
