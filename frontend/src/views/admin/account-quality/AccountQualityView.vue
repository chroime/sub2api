<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import AccountQualityCell from '@/components/admin/account/AccountQualityCell.vue'
import AccountQualityDetail from '@/components/admin/account/AccountQualityDetail.vue'
import AccountQualityRunDetail from '@/components/admin/account/AccountQualityRunDetail.vue'
import QualityPolicyDialog from './QualityPolicyDialog.vue'
import QualityAssignmentDialog from './QualityAssignmentDialog.vue'
import QualitySettings from './QualitySettings.vue'
import api, { type Binding, type ControlAction, type Overview, type Policy, type Run } from '@/api/admin/account-quality'
import { dateTime, duration, newSettings, qualityError, queueChecks, requestId } from './quality'

const emit = defineEmits<{ busy: [boolean] }>()
const { t } = useI18n(), route = useRoute()
const overview = ref<Overview>({ policies: [], bindings: [], templates: [], capabilities: [], settings: newSettings() })
const loading = ref(false), error = ref(''), notice = ref(''), tab = ref('accounts'), search = ref(''), state = ref(''), policyFilter = ref('')
const queueing = ref(false), settingsBusy = ref(false), selected = ref<number[]>([]), editPolicy = ref<Policy | null>(null), policyDialog = ref(false), assignment = ref(false), assignmentPolicy = ref(0), deleting = ref<Policy | null>(null), deletingBusy = ref(false)
const policyBusy = ref(false), assignmentBusy = ref(false), detailBusy = ref(false), evidenceBusy = ref(false)
const accountId = ref(0), initialRun = ref(''), evidence = ref(''), history = ref<Run[]>([]), historyLoading = ref(false)
const initialBindingId = ref(0), initialAction = ref<ControlAction | undefined>()
let generation = 0, historyGeneration = 0
const mutationBusy = computed(() => queueing.value || settingsBusy.value || deletingBusy.value || policyBusy.value || assignmentBusy.value || detailBusy.value || evidenceBusy.value)
watch(mutationBusy, value => emit('busy', value))
const label = (kind: string, value?: string) => { const key = `accountQuality.${kind}_${value}`; const result = t(key); return result === key ? value || '—' : result }
const filtered = computed(() => overview.value.bindings.filter(binding => (!state.value || binding.state === state.value) && (!policyFilter.value || binding.policy_id === Number(policyFilter.value)) && (!search.value.trim() || `${binding.account_name} ${binding.account_id} ${binding.model} ${binding.platform}`.toLowerCase().includes(search.value.trim().toLowerCase()))))
const states = computed(() => [...new Set(overview.value.bindings.map(binding => binding.state))])
const stateOptions = computed(() => [
  { value: '', label: t('accountQuality.allStates') },
  ...states.value.map(item => ({ value: item, label: label('state', item) })),
])
const policyOptions = computed(() => [
  { value: '', label: t('accountQuality.allPolicies') },
  ...overview.value.policies.map(policy => ({ value: String(policy.id), label: policy.name })),
])
const counters = computed(() => [
  { label: 'assigned', value: new Set(overview.value.bindings.map(binding => binding.account_id)).size, tone: 'bg-primary-500', text: 'text-gray-900 dark:text-dark-50' },
  { label: 'state_healthy', value: overview.value.bindings.filter(binding => binding.state === 'healthy').length, tone: 'bg-emerald-500', text: 'text-emerald-700 dark:text-emerald-400' },
  { label: 'state_pending_review', value: overview.value.bindings.filter(binding => binding.failures > 0 && !binding.isolated).length, tone: 'bg-amber-500', text: 'text-amber-700 dark:text-amber-400' },
  { label: 'state_quarantined', value: overview.value.bindings.filter(binding => binding.isolated).length, tone: 'bg-red-500', text: 'text-red-700 dark:text-red-400' },
])
async function load() {
  const request = ++generation; loading.value = true; error.value = ''
  try {
    const value = await api.overview()
    if (request === generation) { overview.value = { ...value, policies: value.policies || [], bindings: value.bindings || [], templates: value.templates || [], capabilities: value.capabilities || [], settings: value.settings || newSettings() }; const currentIds = new Set(overview.value.bindings.map(binding => binding.id)); selected.value = selected.value.filter(id => currentIds.has(id)) }
  } catch (cause) { if (request === generation) error.value = qualityError(cause) || t('accountQuality.loadFailed') }
  finally { if (request === generation) loading.value = false }
}
async function loadHistory() {
  const request = ++historyGeneration; historyLoading.value = true
  try { const value = await api.runs({ limit: 100 }); if (request === historyGeneration) history.value = value || [] }
  catch (cause) { if (request === historyGeneration) error.value = qualityError(cause) || t('accountQuality.loadFailed') }
  finally { if (request === historyGeneration) historyLoading.value = false }
}
async function queue(bindings: Binding[]) {
  if (queueing.value || !bindings.length) return
  queueing.value = true; error.value = ''; notice.value = ''
  const { success, failed } = await queueChecks(bindings, binding => api.run(binding.id, { version: binding.version, request_id: requestId() }))
  notice.value = failed ? t('accountQuality.partial', { success, failed }) : t('accountQuality.queued')
  queueing.value = false; await load(); if (tab.value === 'history') await loadHistory()
}
function toggle(id: number) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id] }
function openPolicy(policy: Policy | null = null) { editPolicy.value = policy; policyDialog.value = true }
function assign(policy = 0) { assignmentPolicy.value = policy; assignment.value = true }
function showAccount(binding: Binding) { initialRun.value = ''; initialBindingId.value = 0; initialAction.value = undefined; accountId.value = binding.account_id }
function showControl(binding: Binding, action: ControlAction) { initialRun.value = ''; initialBindingId.value = binding.id; initialAction.value = action; accountId.value = binding.account_id }
async function policySaved() { policyDialog.value = false; notice.value = t('accountQuality.saved'); await load() }
async function assigned(count: number) { assignment.value = false; notice.value = t('accountQuality.assignedSuccess', { count }); await load() }
async function removePolicy() {
  if (!deleting.value || deletingBusy.value) return
  deletingBusy.value = true; error.value = ''
  try { await api.deletePolicy(deleting.value.id, deleting.value.version); deleting.value = null; await load() }
  catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { deletingBusy.value = false }
}
watch(tab, value => { if (value === 'history') void loadHistory() })
watch(() => [route?.query.account, route?.query.run], ([account, run]) => {
  if (typeof account === 'string' && /^[1-9]\d*$/.test(account) && Number.isSafeInteger(Number(account))) { initialRun.value = typeof run === 'string' ? run : ''; initialBindingId.value = 0; initialAction.value = undefined; accountId.value = Number(account) }
  else if (typeof run === 'string' && run) evidence.value = run
}, { immediate: true })
onMounted(load)
onUnmounted(() => { generation++; historyGeneration++; emit('busy', false) })
</script>

<template>
  <div class="min-w-0 max-w-full space-y-5" data-test="account-quality-view">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0">
        <h3 class="text-lg font-semibold tracking-tight text-gray-900 dark:text-dark-50">{{ t('accountQuality.title') }}</h3>
        <p class="mt-1 max-w-3xl text-xs leading-relaxed text-gray-500 dark:text-dark-300">{{ t('accountQuality.description') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || mutationBusy" @click="load(); tab === 'history' && loadHistory()"><Icon name="refresh" size="sm" class="mr-1.5" />{{ t('accountQuality.refresh') }}</button>
        <button type="button" data-test="quality-new-policy" class="btn btn-secondary btn-sm" :disabled="mutationBusy" @click="openPolicy()"><Icon name="plus" size="sm" class="mr-1.5" />{{ t('accountQuality.createPolicy') }}</button>
        <button type="button" class="btn btn-primary btn-sm" :disabled="!overview.policies.length || mutationBusy" @click="assign()">{{ t('accountQuality.assignment') }}</button>
      </div>
    </header>
    <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
    <p v-if="notice" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300">{{ notice }}</p>
    <dl class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <div v-for="counter in counters" :key="counter.label" :data-test="`quality-counter-${counter.label}`" class="flex items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-dark-600 dark:bg-dark-800">
        <div>
          <dt class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-300"><span class="h-1.5 w-1.5 rounded-full" :class="counter.tone" aria-hidden="true" />{{ t(`accountQuality.${counter.label}`) }}</dt>
          <dd class="mt-1.5 text-2xl font-semibold leading-none tabular-nums" :class="counter.text">{{ counter.value }}</dd>
        </div>
      </div>
    </dl>
    <div class="min-w-0 rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
      <nav class="flex flex-wrap gap-1 border-b border-gray-200 px-3 dark:border-dark-600" :aria-label="t('accountQuality.title')">
        <button v-for="item in ['accounts', 'policies', 'history', 'settings']" :key="item" type="button" class="-mb-px flex items-center gap-2 border-b-2 px-3 py-3 text-sm transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500" :class="tab === item ? 'border-primary-500 font-medium text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-dark-300 dark:hover:text-dark-100'" :aria-pressed="tab === item" :data-test="`quality-tab-${item}`" :disabled="mutationBusy" @click="tab = item">
          {{ t(`accountQuality.${item}`) }}
          <span v-if="item === 'accounts' || item === 'policies'" class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] leading-none tabular-nums text-gray-500 dark:bg-dark-700 dark:text-dark-300">{{ item === 'accounts' ? overview.bindings.length : overview.policies.length }}</span>
        </button>
      </nav>
      <p v-if="loading" role="status" class="px-4 py-3 text-sm text-gray-500">{{ t('accountQuality.loading') }}</p>
      <section v-if="tab === 'accounts'" class="min-w-0">
        <div class="flex flex-wrap items-center gap-2 border-b border-gray-100 p-3 dark:border-dark-700">
          <div class="relative w-full min-w-0 sm:w-56">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" aria-hidden="true" />
            <input v-model="search" class="input w-full pl-9 text-sm" :placeholder="t('accountQuality.search')" :aria-label="t('accountQuality.search')" />
          </div>
          <Select v-model="state" data-test="quality-state-filter" class="w-full text-sm sm:w-40" :options="stateOptions" :aria-label="t('accountQuality.state')" />
          <Select v-model="policyFilter" data-test="quality-policy-filter" class="w-full text-sm sm:w-48" :options="policyOptions" :aria-label="t('accountQuality.policy')" />
          <button v-if="selected.length" type="button" class="btn btn-secondary btn-sm" :disabled="queueing" @click="queue(overview.bindings.filter(binding => selected.includes(binding.id)))">{{ t('accountQuality.queueAll') }} · {{ selected.length }}</button>
          <p class="ml-auto text-xs tabular-nums text-gray-400 dark:text-dark-300">{{ t('accountQuality.monitoredCount', { count: filtered.length }) }}</p>
        </div>
        <p v-if="!filtered.length && !loading" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-dark-300">{{ t(overview.bindings.length ? 'accountQuality.noMatch' : 'accountQuality.empty') }}</p>
        <div v-else class="min-w-0 max-w-full overflow-x-auto rounded-b-xl focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" role="region" tabindex="0" :aria-label="t('accountQuality.accounts')" data-test="quality-monitor-table">
          <table class="w-full min-w-[74rem] table-fixed text-left text-sm">
            <caption class="sr-only">{{ t('accountQuality.accounts') }}</caption>
            <colgroup><col class="w-12" /><col class="w-48" /><col class="w-[36rem]" /><col class="w-48" /><col class="w-44" /></colgroup>
            <thead class="border-b border-gray-200 bg-gray-50/80 text-xs font-medium text-gray-500 dark:border-dark-600 dark:bg-dark-900/50 dark:text-dark-300">
              <tr><th scope="col" class="p-3"><span class="sr-only">{{ t('accountQuality.select') }}</span></th><th scope="col" class="px-4 py-3">{{ t('accountQuality.account') }}</th><th scope="col" class="px-4 py-3">{{ t('accountQuality.monitoring') }}</th><th scope="col" class="px-4 py-3">{{ t('accountQuality.policy') }}</th><th scope="col" class="px-4 py-3">{{ t('accountQuality.nextRun') }}</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="binding in filtered" :key="binding.id" :data-test="`quality-monitor-binding-${binding.id}`" class="transition-colors hover:bg-gray-50/80 dark:hover:bg-dark-700/30" :class="selected.includes(binding.id) ? 'bg-primary-50/50 dark:bg-primary-900/10' : 'even:bg-gray-50/40 dark:even:bg-dark-900/20'">
                <td class="p-3 align-middle"><input type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" :checked="selected.includes(binding.id)" :aria-label="`${t('accountQuality.select')} ${binding.account_name}`" @change="toggle(binding.id)" /></td>
                <td class="px-4 py-4 align-middle">
                  <button type="button" class="max-w-full break-words text-left font-semibold text-gray-900 hover:text-primary-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:text-dark-50 dark:hover:text-primary-400" @click="showAccount(binding)">{{ binding.account_name }}</button>
                  <p class="mt-1 text-[11px] tabular-nums text-gray-400 dark:text-dark-300">#{{ binding.account_id }}</p>
                  <p class="mt-2 flex flex-wrap items-center gap-1 text-[10px] font-medium"><span class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-dark-700 dark:text-dark-200">{{ binding.platform }}</span><span v-if="binding.account_type" class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-dark-700 dark:text-dark-200">{{ binding.account_type }}</span></p>
                  <p class="mt-1.5 break-all text-[11px] text-gray-500 dark:text-dark-300">{{ binding.model }}</p>
                </td>
                <td class="px-4 py-4 align-middle"><AccountQualityCell :bindings="[binding]" :disabled="queueing" @details="showAccount(binding)" @run="queue([$event])" @evidence="evidence = $event.id" @control="showControl" /></td>
                <td class="px-4 py-4 align-middle">
                  <p class="break-words font-medium text-gray-700 dark:text-dark-100">{{ binding.policy_name }}</p>
                  <p class="mt-1 text-[11px] tabular-nums text-gray-400 dark:text-dark-300">v{{ binding.policy_version }}</p>
                  <p class="mt-2 break-all text-[11px] leading-relaxed text-gray-500 dark:text-dark-300">{{ binding.source || '—' }}</p>
                </td>
                <td class="px-4 py-4 align-middle"><div class="flex items-start gap-1.5 text-[11px] leading-relaxed tabular-nums text-gray-500 dark:text-dark-300"><Icon name="clock" size="xs" class="mt-0.5 shrink-0 text-gray-400" aria-hidden="true" /><time>{{ dateTime(binding.next_run_at) }}</time></div></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <section v-if="tab === 'policies'" class="space-y-3 p-4">
        <p v-if="!overview.policies.length" class="py-8 text-center text-sm text-gray-500 dark:text-dark-300">{{ t('accountQuality.noPolicies') }}</p>
        <article v-for="policy in overview.policies" :key="policy.id" class="rounded-xl border border-gray-200 dark:border-dark-600">
          <div class="flex flex-wrap items-start justify-between gap-3 p-4">
            <div class="min-w-0 space-y-1.5">
              <div class="flex flex-wrap items-center gap-2"><h4 class="break-words font-semibold text-gray-900 dark:text-dark-50">{{ policy.name }}</h4><span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium" :class="policy.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'"><span class="h-1 w-1 rounded-full bg-current" aria-hidden="true" />{{ t(policy.enabled ? 'accountQuality.active' : 'accountQuality.disabled') }}</span></div>
              <p class="break-all text-xs text-gray-500 dark:text-dark-300">{{ policy.platform }} · {{ policy.model }} · {{ policy.api_mode }}<span v-if="policy.effort"> · {{ policy.effort }}</span></p>
            </div>
            <div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="mutationBusy" @click="assign(policy.id)">{{ t('accountQuality.assignment') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="mutationBusy" @click="openPolicy(policy)">{{ t('accountQuality.edit') }}</button><button type="button" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" :disabled="mutationBusy" @click="deleting = policy">{{ t('accountQuality.delete') }}</button></div>
          </div>
          <dl class="grid grid-cols-2 gap-x-4 gap-y-3 border-t border-gray-100 bg-gray-50/50 px-4 py-3 text-xs sm:grid-cols-4 dark:border-dark-700 dark:bg-dark-900/30"><div><dt class="text-[11px] text-gray-400 dark:text-dark-300">{{ t('accountQuality.controlMode') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-dark-100">{{ t(`accountQuality.${policy.control_mode}`) }}</dd></div><div><dt class="text-[11px] text-gray-400 dark:text-dark-300">{{ t('accountQuality.interval') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-700 dark:text-dark-100">{{ policy.interval_seconds }} {{ t('accountQuality.seconds') }}</dd></div><div><dt class="text-[11px] text-gray-400 dark:text-dark-300">{{ t('accountQuality.failures') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-700 dark:text-dark-100">{{ policy.failure_threshold }}</dd></div><div><dt class="text-[11px] text-gray-400 dark:text-dark-300">{{ t('accountQuality.assigned') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-700 dark:text-dark-100">{{ overview.bindings.filter(binding => binding.policy_id === policy.id).length }}</dd></div></dl>
        </article>
      </section>
      <section v-if="tab === 'history'" class="space-y-2 p-4">
        <p v-if="historyLoading" role="status" class="text-sm text-gray-500">{{ t('accountQuality.loading') }}</p>
        <p v-else-if="!history.length" class="py-8 text-center text-sm text-gray-500 dark:text-dark-300">{{ t('accountQuality.noRuns') }}</p>
        <button v-for="run in history" :key="run.id" type="button" class="flex w-full flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 text-left text-sm transition-colors hover:bg-gray-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-dark-600 dark:hover:bg-dark-700" @click="evidence = run.id"><span class="min-w-0"><span class="font-medium">{{ run.account_name || `#${run.account_id}` }}</span><span class="ml-2 break-all text-gray-500 dark:text-dark-300">{{ run.model }} · {{ run.template }}</span><time class="mt-1 block text-xs text-gray-500 dark:text-dark-300">{{ dateTime(run.finished_at || run.started_at || run.created_at) }}</time></span><span class="text-xs">{{ label('execution', run.execution_status) }} · {{ label('verdict', run.quality_verdict) }} · {{ duration(run.duration_ms) }}</span></button>
      </section>
      <div v-if="tab === 'settings' && !loading" class="p-4"><QualitySettings :key="overview.settings.version" :settings="overview.settings" @saved="overview.settings = $event" @busy="settingsBusy = $event" /></div>
    </div>
    <QualityPolicyDialog v-if="policyDialog" :policy="editPolicy" :templates="overview.templates" :capabilities="overview.capabilities" @close="policyDialog = false" @saved="policySaved" @busy="policyBusy = $event" />
    <QualityAssignmentDialog v-if="assignment" :policies="overview.policies" :policy-id="assignmentPolicy" @close="assignment = false" @saved="assigned" @busy="assignmentBusy = $event" />
    <AccountQualityDetail v-if="accountId" :key="accountId" :account-id="accountId" :initial-run-id="initialRun" :initial-binding-id="initialBindingId" :initial-action="initialAction" @close="accountId = 0" @changed="load" @busy="detailBusy = $event" />
    <AccountQualityRunDetail v-if="evidence" :run-id="evidence" @close="evidence = ''" @changed="loadHistory" @busy="evidenceBusy = $event" />
    <BaseDialog v-if="deleting" :show="true" :title="t('accountQuality.delete')" width="narrow" :show-close-button="!deletingBusy" @close="deleting = null"><p class="text-sm leading-relaxed">{{ t('accountQuality.deleteHint') }}</p><p class="mt-2 font-semibold">{{ deleting.name }}</p><template #footer><button type="button" class="btn btn-secondary" :disabled="deletingBusy" @click="deleting = null">{{ t('accountQuality.cancel') }}</button><button type="button" class="btn btn-danger" :disabled="deletingBusy" @click="removePolicy">{{ t('accountQuality.delete') }}</button></template></BaseDialog>
  </div>
</template>
