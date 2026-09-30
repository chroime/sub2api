<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type KeyRepairResult, type KeySelection, type ManagedKey } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  siteId: number
  siteVersion?: number
  snapshotId: number
  groups: { id: string; name: string }[]
  selections: { remote_group_id: string; platform: KeySelection['platform'] | '' }[]
  disabled?: boolean
}>()
const emit = defineEmits<{ busy: [value: boolean]; repaired: [] }>()
const { t } = useI18n()
const keys = ref<ManagedKey[]>([])
const secrets = ref<Record<number, string>>({})
const keyErrors = ref<Record<number, string>>({})
const outcomes = ref<{ remote_group_id: string; platform: string; status: string; error?: string }[]>([])
const busy = ref(false), loading = ref(false), error = ref(''), copied = ref(false)
const repairTarget = ref<ManagedKey | null>(null)
const repairPlan = ref<KeyRepairResult | null>(null)
const repairError = ref('')
const acknowledgeAbandon = ref(false)
const processed = ref(0), total = ref(0)
let generation = 0
const groupName = (id: string) => props.groups.find(group => group.id === id)?.name || id
const keyStatus: Record<string, string> = { created: 'keyCreated', reused: 'keyReused', failed: 'failed', pending: 'keyWaiting', interrupted: 'keyInterrupted', not_attempted: 'keyNotAttempted' }
const copyable = computed(() => keys.value.filter(key => !!secrets.value[key.id]))
const validSelection = computed(() => !!props.selections.length && props.selections.every(selection => !!selection.platform) && props.selections.length <= 100)
function setBusy(value: boolean) {
  if (busy.value === value) return
  busy.value = value
  emit('busy', value)
}
function clearSecrets() { secrets.value = {}; keyErrors.value = {}; copied.value = false }
watch(() => [props.siteId, props.snapshotId], async () => {
  const request = ++generation
  clearSecrets()
  repairTarget.value = null
  repairPlan.value = null
  repairError.value = ''
  acknowledgeAbandon.value = false
  keys.value = []
  outcomes.value = []
  error.value = ''
  setBusy(false)
  loading.value = true
  try {
    const metadata = await api.keys(props.siteId)
    if (request !== generation) return
    keys.value = metadata
    for (const key of metadata) {
      if (request !== generation) return
      if (key.has_key) await readKey(key, request)
    }
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) loading.value = false
  }
}, { immediate: true })
onUnmounted(() => { generation++; clearSecrets(); keys.value = []; outcomes.value = []; repairTarget.value = null; repairPlan.value = null; acknowledgeAbandon.value = false; setBusy(false) })
function remember(key: ManagedKey, plaintext?: string) {
  const current = keys.value.find(existing => existing.id === key.id)
  keys.value = [...keys.value.filter(existing => existing.id !== key.id), { ...key, health: key.health ?? current?.health }]
  if (plaintext) {
    secrets.value[key.id] = plaintext
    delete keyErrors.value[key.id]
  }
}
async function audit() {
  if (busy.value || loading.value || props.disabled) return
  const request = generation
  setBusy(true)
  error.value = ''
  try {
    const metadata = await api.auditKeys(props.siteId)
    if (request === generation) keys.value = metadata
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function openRepair(key: ManagedKey) {
  if (busy.value || loading.value || props.disabled || !props.siteVersion) return
  const request = generation
  repairTarget.value = key
  repairPlan.value = null
  repairError.value = ''
  acknowledgeAbandon.value = false
  setBusy(true)
  try {
    const existing = await api.keyRepair(props.siteId, key.id)
    if (request === generation) repairPlan.value = existing
  } catch (e) {
    if (request === generation) repairError.value = t(errorKey(e))
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function prepareRepair() {
  const key = repairTarget.value
  if (!key || busy.value || !props.siteVersion || repairPlan.value && !repairPlan.value.can_reprepare && repairPlan.value.stage !== 'committed') return
  const request = generation
  const siteId = props.siteId
  const priorPlanId = repairPlan.value?.id
  setBusy(true)
  repairError.value = ''
  try {
    const plan = await api.prepareKeyRepair(siteId, key.id, { site_version: props.siteVersion })
    if (request === generation) repairPlan.value = plan
  } catch (e) {
    let recovered: KeyRepairResult | null = null
    try {
      recovered = await api.keyRepair(siteId, key.id)
    } catch { /* Preserve the original preparation error. */ }
    if (request === generation) {
      if (recovered && recovered.id !== priorPlanId) repairPlan.value = recovered
      else repairError.value = t(errorKey(e))
    }
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function confirmRepair() {
  const key = repairTarget.value, plan = repairPlan.value
  if (!key || !plan || busy.value || plan.stage === 'committed' || plan.stage === 'conflict' || plan.stage === 'abandoned') return
  const request = generation
  const siteId = props.siteId
  setBusy(true)
  repairError.value = ''
  try {
    const result = await api.confirmKeyRepair(siteId, key.id, plan.id)
    if (request !== generation) return
    repairPlan.value = result
    if (result.stage === 'committed') {
      clearSecrets()
      keys.value = []
      emit('repaired')
      const updated = await api.keys(siteId)
      if (request !== generation) return
      keys.value = updated
      for (const item of keys.value) {
        if (request !== generation) return
        if (item.has_key) await readKey(item, request)
      }
    }
  } catch (e) {
    if (request === generation) repairError.value = t(errorKey(e))
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function abandonRepair() {
  const key = repairTarget.value, plan = repairPlan.value
  if (!key || !plan?.can_abandon || !acknowledgeAbandon.value || busy.value) return
  const request = generation
  setBusy(true)
  repairError.value = ''
  try {
    const result = await api.abandonKeyRepair(props.siteId, key.id, plan.id, { acknowledge_uncertain_create: true })
    if (request === generation) {
      repairPlan.value = result
      acknowledgeAbandon.value = false
    }
  } catch (e) {
    if (request === generation) repairError.value = t(errorKey(e))
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function create(selections = props.selections) {
  if (busy.value || props.disabled || !selections.length || selections.length > 100) return
  if (selections.some(selection => !selection.platform)) { error.value = t('governance.chooseTransport'); return }
  const request = generation
  setBusy(true)
  error.value = ''
  copied.value = false
  processed.value = 0
  total.value = selections.length
  outcomes.value = selections.map(selection => ({ ...selection, status: 'pending' }))
  let inFlight: typeof selections = []
  try {
    for (let offset = 0; offset < selections.length; offset += 10) {
      inFlight = selections.slice(offset, offset + 10)
      const result = await api.createKeys(props.siteId, {
        snapshot_id: props.snapshotId,
        selections: inFlight.map(selection => ({ remote_group_id: selection.remote_group_id, platform: selection.platform as KeySelection['platform'] })),
      })
      if (request !== generation) return
      for (const item of result.items) {
        const index = outcomes.value.findIndex(outcome => outcome.remote_group_id === item.remote_group_id && outcome.platform === item.platform)
        const { remote_group_id, platform, status, error: itemError } = item
        if (index >= 0) outcomes.value[index] = { remote_group_id, platform, status, error: itemError }
        if (item.managed_key) remember(item.managed_key, item.key)
      }
      processed.value += inFlight.length
    }
  } catch (e) {
    if (request === generation) {
      error.value = t(errorKey(e))
      outcomes.value = outcomes.value.map(outcome => outcome.status !== 'pending' ? outcome : {
        ...outcome,
        status: inFlight.some(selection => selection.remote_group_id === outcome.remote_group_id && selection.platform === outcome.platform) ? 'interrupted' : 'not_attempted',
      })
    }
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function readKey(key: ManagedKey, request: number) {
  delete keyErrors.value[key.id]
  try {
    const result = await api.revealKey(props.siteId, key.id)
    if (request !== generation) return
    if (!result.key) { keyErrors.value[key.id] = t('governance.keyUnavailable'); return }
    remember(result.managed_key, result.key)
  } catch (e) {
    if (request === generation) keyErrors.value[key.id] = t(errorKey(e))
  }
}
async function reveal(key: ManagedKey) {
  if (busy.value || loading.value || props.disabled || !key.has_key) return
  const request = generation
  setBusy(true)
  error.value = ''
  copied.value = false
  try {
    await readKey(key, request)
  } finally {
    if (request === generation) setBusy(false)
  }
}
async function copy(key?: ManagedKey) {
  const plaintext = key ? secrets.value[key.id] : copyable.value.map(item => secrets.value[item.id]).join('\n')
  if (!plaintext) return
  const request = generation
  try {
    await navigator.clipboard.writeText(plaintext)
    if (request === generation) copied.value = true
  } catch {
    if (request === generation) error.value = t('governance.copyFailed')
  }
}
</script>

<template>
  <section id="governance-managed-keys" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div><h4 class="font-semibold">{{ t('governance.groupKeys') }}</h4><p class="mt-1 text-sm text-gray-500">{{ t('governance.groupKeysHint') }}</p></div>
      <div class="flex flex-wrap gap-2">
        <button data-test="audit-keys" type="button" class="btn btn-secondary" :disabled="busy || disabled || loading" @click="audit()">{{ t('governance.auditKeys') }}</button>
        <button id="governance-create-keys" data-test="create-keys" type="button" class="btn btn-secondary" :disabled="busy || disabled || loading || !validSelection" @click="create()">{{ t('governance.createSelectedKeys') }}</button>
      </div>
    </div>
    <p class="text-xs text-gray-500">{{ t('governance.keyLimits') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="loading" role="status" class="text-sm">{{ t('common.loading') }}</p>
    <p v-else-if="!keys.length && !selections.length" class="text-sm text-gray-500">{{ t('governance.selectForKeys') }}</p>
    <details v-if="selections.length" class="text-xs"><summary class="cursor-pointer text-gray-500">{{ t('governance.individualKeys') }} · {{ selections.length }}</summary><div class="mt-3 flex flex-wrap gap-2">
      <button v-for="selection in selections" :key="selection.remote_group_id + selection.platform" type="button" class="btn btn-secondary text-sm" data-test="create-group-key" :disabled="busy || disabled || loading || !selection.platform" @click="create([selection])">
        {{ t('governance.createGroupKey', { name: groupName(selection.remote_group_id) }) }} · {{ selection.platform || t('governance.chooseTransport') }}
      </button>
    </div></details>
    <p v-if="busy && total" role="status" class="text-sm text-primary-600">{{ t('governance.keyBatchProgress', { count: processed, total }) }}</p>
    <p v-for="item in outcomes" :key="item.remote_group_id + item.platform" role="status" class="text-sm" :class="item.status === 'failed' || item.status === 'interrupted' ? 'text-red-600' : item.status === 'created' || item.status === 'reused' ? 'text-emerald-700' : 'text-gray-500'">
      {{ groupName(item.remote_group_id) }} / {{ item.platform }}: {{ t('governance.' + (keyStatus[item.status] || 'unknown')) }}<span v-if="item.error"> · {{ t(errorKey({ reason: item.error })) }}</span>
    </p>
    <article v-for="key in keys" :key="key.id" class="space-y-2 rounded-lg border p-3 dark:border-dark-600">
      <div class="flex flex-wrap items-center gap-2">
        <strong class="mr-auto text-sm">{{ groupName(key.remote_group_id) }} · {{ key.platform }}</strong>
        <button v-if="key.has_key && siteVersion && key.health?.status === 'confirmed_missing'" data-test="repair-key" type="button" class="btn btn-secondary text-xs" :disabled="busy || disabled || loading" @click="openRepair(key)">{{ t('governance.repairKey') }}</button>
        <template v-if="key.has_key">
          <button v-if="keyErrors[key.id]" :id="'governance-reveal-key-' + key.id" data-test="reveal-key" type="button" class="btn btn-secondary" :disabled="busy || disabled || loading" @click="reveal(key)">{{ t('governance.rereadKey') }}</button>
          <button v-if="secrets[key.id]" data-test="copy-key" type="button" class="btn btn-secondary" @click="copy(key)">{{ t('governance.copyKey') }}</button>
        </template>
        <span v-else class="text-sm text-amber-700">{{ t('governance.keyPending') }}</span>
      </div>
      <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs">
        <p data-test="key-local-state" class="text-gray-600 dark:text-gray-300">{{ t('governance.localKeyState') }}: {{ t(key.has_key ? 'governance.keySavedLocally' : 'governance.keyNotSavedLocally') }}</p>
        <p data-test="key-remote-state" :class="key.health?.status === 'confirmed_missing' || key.health?.status === 'group_changed' ? 'font-medium text-red-600' : key.health?.status === 'suspected_missing' ? 'font-medium text-amber-700 dark:text-amber-400' : 'text-gray-600 dark:text-gray-300'">{{ t('governance.remoteKeyState') }}: {{ t(`governance.keyHealth_${key.health?.status ?? 'unknown'}`) }}</p>
      </div>
      <p data-test="key-health-checked" class="text-xs tabular-nums text-gray-500">{{ t('governance.keyLastChecked') }} {{ formatGovernanceTime(key.health?.last_checked_at) }}</p>
      <p v-if="key.health?.error_code" data-test="key-health-error" role="status" class="text-xs text-amber-700 dark:text-amber-400">{{ t('governance.keyAuditFailed') }} · {{ t(errorKey({ reason: key.health.error_code })) }}</p>
      <p class="text-[11px] tabular-nums text-gray-400">{{ t('governance.keyCreatedAt') }} {{ formatGovernanceTime(key.created_at) }} · {{ t('governance.keyUpdatedAt') }} {{ formatGovernanceTime(key.updated_at) }}</p>
      <code v-if="secrets[key.id]" data-test="key-secret" class="block select-all break-all rounded bg-gray-100 p-3 text-sm dark:bg-dark-900">{{ secrets[key.id] }}</code>
      <p v-else-if="keyErrors[key.id]" data-test="key-read-error" role="alert" class="text-sm text-red-600">{{ t('governance.keyReadFailed') }} · {{ keyErrors[key.id] }}</p>
      <p v-else-if="key.has_key" class="text-sm text-gray-500">{{ t('governance.keyLoading') }}</p>
    </article>
    <section v-if="repairTarget" data-test="repair-plan" class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
      <div class="flex items-center justify-between gap-2">
        <h4 class="text-sm font-semibold">{{ t('governance.repairKey') }} · {{ groupName(repairTarget.remote_group_id) }}</h4>
        <button type="button" :title="t('common.close')" :aria-label="t('common.close')" class="flex h-10 w-10 items-center justify-center text-gray-500 hover:text-gray-900 dark:hover:text-white" :disabled="busy" @click="repairTarget = null; repairPlan = null; acknowledgeAbandon = false"><Icon name="x" size="sm" /></button>
      </div>
      <p v-if="repairError" role="alert" class="text-sm text-red-600">{{ repairError }}</p>
      <p v-if="repairPlan" data-test="repair-stage" class="text-sm">{{ t(`governance.keyRepairStage_${repairPlan.stage}`) }}<span v-if="repairPlan.error_code"> · {{ t(errorKey({ reason: repairPlan.error_code })) }}</span></p>
      <p v-if="repairPlan" class="text-xs text-gray-500">{{ repairPlan.account_name }} #{{ repairPlan.account_id }} · {{ t('governance.keyOldRemoteID') }} #{{ repairPlan.old_remote_key_id }}</p>
      <p v-if="repairPlan?.planned_key_name" class="text-xs text-gray-500">{{ t('governance.plannedKeyName') }} <code data-test="planned-key-name" class="select-all break-all font-medium text-gray-800 dark:text-gray-200">{{ repairPlan.planned_key_name }}</code></p>
      <div class="flex flex-wrap gap-2">
        <button v-if="!repairPlan || repairPlan.can_reprepare || repairPlan.stage === 'committed'" data-test="prepare-key-repair" type="button" class="btn btn-secondary" :disabled="busy" @click="prepareRepair">{{ t('governance.prepareKeyRepair') }}</button>
        <button v-else-if="repairPlan.stage !== 'conflict' && repairPlan.stage !== 'abandoned'" data-test="confirm-key-repair" type="button" class="btn btn-primary" :disabled="busy" @click="confirmRepair">{{ t(repairPlan.stage === 'prepared' ? 'governance.confirmKeyRepair' : 'governance.recheckKeyRepair') }}</button>
      </div>
      <div v-if="repairPlan?.can_abandon" class="space-y-2 border-t border-gray-200 pt-3 dark:border-dark-600">
        <label class="governance-checkbox-label flex items-start gap-2 text-xs leading-5 text-amber-800 dark:text-amber-300"><input v-model="acknowledgeAbandon" data-test="acknowledge-key-repair" type="checkbox" class="governance-checkbox mt-0.5" /><span>{{ t('governance.abandonKeyRepairWarning') }}</span></label>
        <button data-test="abandon-key-repair" type="button" class="btn btn-secondary text-xs" :disabled="busy || !acknowledgeAbandon" @click="abandonRepair">{{ t('governance.abandonKeyRepair') }}</button>
      </div>
    </section>
    <div v-if="copyable.length" class="flex flex-wrap items-center gap-2">
      <button id="governance-copy-all-keys" data-test="copy-all-keys" type="button" class="btn btn-secondary" @click="copy()">{{ t('governance.copyAllKeys') }}</button>
      <p class="text-xs text-gray-500">{{ t('governance.transientKeys') }}</p>
    </div>
    <p v-if="copied" role="status" class="text-sm text-emerald-700">{{ t('governance.copied') }}</p>
  </section>
</template>
