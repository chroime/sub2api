<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import api, { type Run } from '@/api/admin/account-quality'
import { dateTime, duration, qualityError } from '@/views/admin/account-quality/quality'

const props = defineProps<{ runId: string }>()
const emit = defineEmits<{ close: []; changed: []; busy: [boolean] }>()
const { t } = useI18n()
const run = ref<Run | null>(null), loading = ref(false), busy = ref(false), error = ref('')
let generation = 0
const label = (kind: string, value?: string) => { const key = `accountQuality.${kind}_${value}`; const result = t(key); return result === key ? value || '—' : result }
async function load() {
  const request = ++generation
  loading.value = true; error.value = ''; run.value = null
  try { const value = await api.runDetail(props.runId); if (request === generation) run.value = value }
  catch (cause) { if (request === generation) error.value = qualityError(cause) || t('accountQuality.loadFailed') }
  finally { if (request === generation) loading.value = false }
}
async function cancel() {
  if (busy.value) return
  busy.value = true; emit('busy', true)
  try { await api.cancelRun(props.runId); emit('changed'); await load() }
  catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { busy.value = false; emit('busy', false) }
}
watch(() => props.runId, load, { immediate: true })
onUnmounted(() => { generation++ })
</script>

<template>
  <BaseDialog :show="true" :title="t('accountQuality.evidence')" width="extra-wide" :z-index="60" :show-close-button="!busy" :close-on-escape="!busy" @close="emit('close')">
    <div class="min-w-0 space-y-4" data-test="quality-run-detail">
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="loading" role="status" class="py-6 text-sm text-gray-500">{{ t('accountQuality.loading') }}</p>
      <template v-if="run">
        <div class="flex flex-wrap items-start justify-between gap-3"><div class="min-w-0"><p class="font-semibold">{{ run.account_name || `#${run.account_id}` }} · {{ run.model }}</p><p class="mt-1 break-all font-mono text-xs text-gray-500">{{ run.id }}</p></div><div class="flex gap-2"><button v-if="['queued', 'running'].includes(run.execution_status)" class="btn btn-secondary btn-sm" :disabled="busy" @click="cancel">{{ t('accountQuality.cancelRun') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">{{ t('accountQuality.refresh') }}</button></div></div>
        <p v-if="run.stale" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('accountQuality.stale') }}</p>
        <p v-if="!run.auto_control_eligible" class="text-xs text-gray-500">{{ t('accountQuality.notEligible') }}</p>
        <dl class="grid gap-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-600 sm:grid-cols-2 lg:grid-cols-3">
          <div v-for="entry in [{ key: 'execution', value: label('execution', run.execution_status) }, { key: 'verdict', value: label('verdict', run.quality_verdict) }, { key: 'actualAccount', value: `#${run.account_id} ${run.account_name || ''}` }, { key: 'credentialAccount', value: run.credential_account_id ? `#${run.credential_account_id}` : null }, { key: 'identityVersion', value: run.identity_version }, { key: 'upstreamRequestId', value: run.upstream_request_id }, { key: 'requestedModel', value: run.requested_model || run.model }, { key: 'mappedModel', value: run.mapped_model }, { key: 'returnedModel', value: run.returned_model }, { key: 'protocol', value: run.api_mode }, { key: 'effort', value: run.effort }, { key: 'outputTokens', value: run.max_output_tokens }, { key: 'outputLimitMode', value: run.output_limit_mode }, { key: 'templateVersion', value: `${run.template} · ${run.template_version}` }, { key: 'version', value: `P${run.policy_version} / B${run.binding_version}` }, { key: 'started', value: dateTime(run.started_at) }, { key: 'finished', value: dateTime(run.finished_at) }, { key: 'duration', value: duration(run.duration_ms) }, { key: 'inputTokens', value: run.input_tokens }, { key: 'outputTokensLabel', value: run.output_tokens }, { key: 'reasoningTokens', value: run.reasoning_tokens }, { key: 'httpStatus', value: run.http_status || null }, { key: 'errorClass', value: run.error_class }, { key: 'finishReason', value: run.finish_reason }, { key: 'cost', value: run.cost_usd }]" :key="entry.key" class="min-w-0"><dt class="text-xs text-gray-500">{{ t(`accountQuality.${entry.key}`) }}</dt><dd class="mt-1 break-all font-medium">{{ entry.value ?? '—' }}</dd></div>
        </dl>
        <p v-if="run.reason" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900">{{ run.reason }}</p>
        <p class="text-xs text-gray-500">{{ t('accountQuality.evidenceHint') }}</p>
        <section v-for="entry in [{ key: 'prompt', value: run.prompt }, { key: 'expected', value: run.expected }, { key: 'output', value: run.response_text }]" :key="entry.key" class="min-w-0"><h4 class="mb-2 text-sm font-semibold">{{ t(`accountQuality.${entry.key}`) }}</h4><pre class="max-h-[28rem] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-relaxed dark:border-dark-600 dark:bg-dark-900">{{ entry.value || '—' }}</pre></section>
        <details v-if="run.config" class="text-sm"><summary class="cursor-pointer font-semibold">{{ t('accountQuality.configSnapshot') }}</summary><pre class="mt-2 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-4 text-xs dark:bg-dark-900">{{ JSON.stringify(run.config, null, 2) }}</pre></details>
      </template>
    </div>
  </BaseDialog>
</template>
