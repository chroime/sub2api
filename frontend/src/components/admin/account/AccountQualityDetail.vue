<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import AccountQualityCell from './AccountQualityCell.vue'
import AccountQualityRunDetail from './AccountQualityRunDetail.vue'
import api, { type AccountDetail, type Binding, type ControlAction } from '@/api/admin/account-quality'
import { dateTime, duration, qualityError, requestId } from '@/views/admin/account-quality/quality'

const props = defineProps<{ accountId: number; initialRunId?: string; initialBindingId?: number; initialAction?: ControlAction }>()
const emit = defineEmits<{ close: []; changed: []; busy: [boolean] }>()
const { t } = useI18n()
const detail = ref<AccountDetail>({ bindings: [], runs: [], actions: [] }), loading = ref(false), busy = ref(false), error = ref(''), notice = ref('')
const tab = ref('summary'), selectedBinding = ref(0), controlAction = ref<ControlAction>('override'), reason = ref(''), allowance = ref(15), automatic = ref(false), evidence = ref(props.initialRunId || '')
const controlForm = ref<HTMLFormElement | null>(null)
const active = computed(() => selectedBinding.value ? detail.value.bindings.find(binding => binding.id === selectedBinding.value) : props.initialBindingId ? undefined : detail.value.bindings[0])
const automaticAllowed = computed(() => Boolean(active.value?.auto_control_eligible && active.value.control_mode === 'auto_quarantine'))
const actions: ControlAction[] = ['isolate', 'release', 'override', 'confirm_false_positive', 'set_auto']
const bindingOptions = computed<SelectOption[]>(() => detail.value.bindings.map(binding => ({ value: binding.id, label: `${binding.policy_name} · ${binding.model}` })))
const actionOptions = computed<SelectOption[]>(() => actions.map(action => ({ value: action, label: t(`accountQuality.action_${action}`) })))
const label = (kind: string, value?: string) => { const key = `accountQuality.${kind}_${value}`; const result = t(key); return result === key ? value || '—' : result }
let generation = 0, initialControlPending = true
function selectBinding(id: number) { selectedBinding.value = id; reason.value = ''; error.value = '' }
async function toggleAutoIsolation(binding: Binding) {
  if (busy.value) return
  const enabled = !binding.auto_isolate
  const bindingAutomaticAllowed = Boolean(binding.auto_control_eligible && binding.control_mode === 'auto_quarantine')
  if (enabled && !bindingAutomaticAllowed) {
    error.value = t(binding.control_mode && binding.control_mode !== 'auto_quarantine' ? 'accountQuality.autoModeRequired' : 'accountQuality.autoUnsupported')
    return
  }
  busy.value = true; error.value = ''; notice.value = ''; emit('busy', true)
  try {
    await api.control(binding.id, { version: binding.version, action: 'set_auto', auto_isolate: enabled, reason: t(enabled ? 'accountQuality.autoEnableReason' : 'accountQuality.autoDisableReason') })
    notice.value = t(enabled ? 'accountQuality.autoEnabled' : 'accountQuality.autoDisabled')
    emit('changed'); await load()
  } catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { busy.value = false; emit('busy', false) }
}
async function openControl(binding: Binding, action: ControlAction) {
  if (action === 'set_auto') {
    await toggleAutoIsolation(binding)
    return
  }
  selectedBinding.value = binding.id; controlAction.value = action; tab.value = 'summary'; reason.value = ''; error.value = ''
  await nextTick()
  controlForm.value?.scrollIntoView?.({ block: 'nearest', behavior: 'auto' })
}
async function load() {
  const request = ++generation; loading.value = true; error.value = ''
  try {
    const value = await api.account(props.accountId)
    if (request === generation) {
      detail.value = { bindings: value.bindings || [], runs: value.runs || [], actions: value.actions || [] }
      if (!selectedBinding.value) selectedBinding.value = props.initialBindingId || detail.value.bindings[0]?.id || 0
      const requested = props.initialBindingId ? detail.value.bindings.find(binding => binding.id === props.initialBindingId) : undefined
      if (selectedBinding.value && !detail.value.bindings.some(binding => binding.id === selectedBinding.value)) error.value = t('accountQuality.bindingUnavailable')
      if (initialControlPending && props.initialAction && actions.includes(props.initialAction) && requested) {
        initialControlPending = false
        await openControl(requested, props.initialAction)
      }
    }
  }
  catch (cause) { if (request === generation) error.value = qualityError(cause) || t('accountQuality.loadFailed') }
  finally { if (request === generation) loading.value = false }
}
async function run(binding: Binding) {
  if (busy.value) return
  busy.value = true; error.value = ''; notice.value = ''; emit('busy', true)
  try { await api.run(binding.id, { version: binding.version, request_id: requestId() }); notice.value = t('accountQuality.queued'); emit('changed'); await load() }
  catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { busy.value = false; emit('busy', false) }
}
async function control() {
  if (busy.value || !active.value) return
  if (!reason.value.trim()) { error.value = t('accountQuality.reasonRequired'); return }
  if (controlAction.value === 'set_auto' && automatic.value && !automaticAllowed.value) { error.value = t(active.value.control_mode && active.value.control_mode !== 'auto_quarantine' ? 'accountQuality.autoModeRequired' : 'accountQuality.autoUnsupported'); return }
  busy.value = true; error.value = ''; notice.value = ''; emit('busy', true)
  try {
    await api.control(active.value.id, { version: active.value.version, action: controlAction.value, reason: reason.value.trim(), ...(controlAction.value === 'override' ? { duration_minutes: allowance.value } : {}), ...(controlAction.value === 'set_auto' ? { auto_isolate: automatic.value } : {}) })
    reason.value = ''; notice.value = t('accountQuality.saved'); emit('changed'); await load()
  } catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { busy.value = false; emit('busy', false) }
}
watch(() => [props.accountId, props.initialBindingId, props.initialAction] as const, () => {
  detail.value = { bindings: [], runs: [], actions: [] }; selectedBinding.value = props.initialBindingId || 0
  controlAction.value = props.initialAction && actions.includes(props.initialAction) ? props.initialAction : 'override'
  tab.value = 'summary'; reason.value = ''; notice.value = ''; initialControlPending = true
  void load()
}, { immediate: true })
watch(active, binding => { automatic.value = binding?.auto_isolate || false })
onUnmounted(() => { generation++ })
</script>

<template>
  <BaseDialog :show="true" :title="`${t('accountQuality.detail')} · #${accountId}`" width="extra-wide" :close-on-escape="!busy" :show-close-button="!busy" @close="emit('close')">
    <div class="min-w-0 space-y-4" data-test="quality-account-detail">
      <div class="flex items-center justify-between gap-3"><nav class="flex flex-wrap gap-2"><button v-for="item in ['summary', 'history', 'actions']" :key="item" type="button" class="rounded-lg px-3 py-2 text-sm" :class="tab === item ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" :aria-pressed="tab === item" @click="tab = item">{{ t(`accountQuality.${item === 'summary' ? 'accounts' : item}`) }}</button></nav><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || busy" @click="load">{{ t('accountQuality.refresh') }}</button></div>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="notice" role="status" class="text-sm text-emerald-600">{{ notice }}</p><p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('accountQuality.loading') }}</p>
      <section v-if="tab === 'summary'" class="space-y-4">
        <p v-if="!detail.bindings.length && !loading" class="rounded-lg border border-dashed border-gray-200 p-6 text-sm text-gray-500 dark:border-dark-600">{{ t('accountQuality.empty') }}</p>
        <div v-for="binding in detail.bindings" :key="binding.id" class="flex flex-wrap items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <div class="min-w-0 max-w-full"><p class="font-semibold">{{ binding.account_name }} · {{ binding.model }}</p><AccountQualityCell :bindings="[binding]" :disabled="busy" @run="run" @details="selectBinding(binding.id)" @evidence="evidence = $event.id" @control="openControl" /></div>
          <dl class="grid gap-3 text-xs sm:grid-cols-2"><div><dt class="text-gray-500">{{ t('accountQuality.source') }}</dt><dd class="mt-1 max-w-xs break-all">{{ binding.source || '—' }}</dd></div><div><dt class="text-gray-500">{{ t('accountQuality.version') }}</dt><dd class="mt-1">P{{ binding.policy_version }} / B{{ binding.version }}</dd></div><div><dt class="text-gray-500">{{ t('accountQuality.nextRun') }}</dt><dd class="mt-1">{{ dateTime(binding.next_run_at) }}</dd></div><div><dt class="text-gray-500">{{ t('accountQuality.recoveryPasses') }}</dt><dd class="mt-1">{{ binding.recovery_passes }}<span v-if="binding.recovery_threshold"> / {{ binding.recovery_threshold }}</span></dd></div><div v-if="binding.override_until"><dt class="text-gray-500">{{ t('accountQuality.overrideUntil') }}</dt><dd class="mt-1">{{ dateTime(binding.override_until) }}</dd></div></dl>
          <p v-if="binding.control_mode" class="w-full text-xs text-gray-500">{{ t('accountQuality.controlMode') }}: {{ t(`accountQuality.${binding.control_mode}`) }} · {{ t('accountQuality.interval') }}: {{ binding.interval_seconds }} {{ t('accountQuality.seconds') }} · {{ t('accountQuality.scope') }}: {{ t(`accountQuality.scope_${binding.isolation_scope || 'model'}`) }}</p>
          <p v-if="binding.reason" class="w-full break-words text-xs text-gray-500">{{ binding.reason }}</p>
          <details v-if="binding.overrides && Object.keys(binding.overrides).length" class="w-full text-xs"><summary class="cursor-pointer">{{ t('accountQuality.overrides') }}</summary><pre class="mt-2 whitespace-pre-wrap">{{ JSON.stringify(binding.overrides, null, 2) }}</pre></details>
        </div>
        <form v-if="active" ref="controlForm" class="space-y-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-900" @submit.prevent="control">
          <p class="text-sm font-semibold">{{ t(`accountQuality.action_${controlAction}`) }} · {{ active.account_name }} · {{ active.policy_name }}</p>
          <p class="text-xs leading-relaxed text-gray-500">{{ t('accountQuality.gateHint') }}</p>
          <div class="grid gap-3 sm:grid-cols-2"><label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.policy') }}</span><Select v-model="selectedBinding" :options="bindingOptions" :searchable="false" @change="reason = ''; error = ''" /></label><label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.actions') }}</span><Select v-model="controlAction" data-test="quality-control-action" :options="actionOptions" :searchable="false" /></label></div>
          <label v-if="controlAction === 'override'" class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.durationMinutes') }}</span><input v-model.number="allowance" class="input w-full sm:w-40" type="number" min="1" max="10080" required /></label>
          <template v-if="controlAction === 'set_auto'"><label class="flex items-center gap-2 text-sm"><input v-model="automatic" data-test="quality-control-auto" type="checkbox" :disabled="!automaticAllowed && !active.auto_isolate" class="rounded" />{{ t('accountQuality.automatic') }}</label><p v-if="!automaticAllowed" class="text-xs text-amber-600">{{ t(active.control_mode && active.control_mode !== 'auto_quarantine' ? 'accountQuality.autoModeRequired' : 'accountQuality.autoUnsupported') }}</p></template>
          <label class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.reason') }}</span><textarea v-model="reason" data-test="quality-control-reason" class="input w-full" rows="2" maxlength="2000" /></label>
          <button type="submit" data-test="quality-control-submit" class="btn btn-primary btn-sm" :disabled="busy || loading">{{ t('accountQuality.applyAction') }}</button>
        </form>
      </section>
      <section v-if="tab === 'history'" class="space-y-2"><p v-if="!detail.runs.length" class="py-6 text-sm text-gray-500">{{ t('accountQuality.noRuns') }}</p><button v-for="runItem in detail.runs" :key="runItem.id" type="button" class="flex w-full flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-200 p-3 text-left text-sm hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700" @click="evidence = runItem.id"><span>{{ runItem.template }} · {{ runItem.model }}<time class="mt-1 block text-xs text-gray-500">{{ dateTime(runItem.finished_at || runItem.started_at || runItem.created_at) }}</time></span><span>{{ label('execution', runItem.execution_status) }} · {{ label('verdict', runItem.quality_verdict) }} · {{ duration(runItem.duration_ms) }}</span></button></section>
      <section v-if="tab === 'actions'" class="space-y-2"><p v-if="!detail.actions.length" class="py-6 text-sm text-gray-500">{{ t('accountQuality.noActions') }}</p><article v-for="action in detail.actions" :key="action.id" class="rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600"><div class="flex flex-wrap justify-between gap-2"><p class="font-medium">{{ label('action', action.action) }}</p><time class="text-xs text-gray-500">{{ dateTime(action.created_at) }}</time></div><p class="mt-1 whitespace-pre-wrap break-words">{{ action.reason }}</p><p class="mt-1 text-xs text-gray-500">{{ t('accountQuality.actor') }}: {{ action.actor_id || '—' }} · #{{ action.binding_id }}</p></article></section>
    </div>
  </BaseDialog>
  <AccountQualityRunDetail v-if="evidence" :run-id="evidence" @close="evidence = ''" @changed="load(); emit('changed')" @busy="emit('busy', $event)" />
</template>
