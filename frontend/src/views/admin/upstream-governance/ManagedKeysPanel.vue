<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type KeySelection, type ManagedKey } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'

const props = defineProps<{
  siteId: number
  snapshotId: number
  groups: { id: string; name: string }[]
  selections: { remote_group_id: string; platform: KeySelection['platform'] | '' }[]
  disabled?: boolean
}>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const { t } = useI18n()
const keys = ref<ManagedKey[]>([])
const secrets = ref<Record<number, string>>({})
const keyErrors = ref<Record<number, string>>({})
const outcomes = ref<{ remote_group_id: string; platform: string; status: string; error?: string }[]>([])
const busy = ref(false), loading = ref(false), error = ref(''), copied = ref(false)
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
onUnmounted(() => { generation++; clearSecrets(); keys.value = []; outcomes.value = []; setBusy(false) })
function remember(key: ManagedKey, plaintext?: string) {
  keys.value = [...keys.value.filter(existing => existing.id !== key.id), key]
  if (plaintext) {
    secrets.value[key.id] = plaintext
    delete keyErrors.value[key.id]
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
      <button id="governance-create-keys" data-test="create-keys" type="button" class="btn btn-secondary" :disabled="busy || disabled || loading || !validSelection" @click="create()">{{ t('governance.createSelectedKeys') }}</button>
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
        <template v-if="key.has_key">
          <button v-if="keyErrors[key.id]" :id="'governance-reveal-key-' + key.id" data-test="reveal-key" type="button" class="btn btn-secondary" :disabled="busy || disabled || loading" @click="reveal(key)">{{ t('governance.rereadKey') }}</button>
          <button v-if="secrets[key.id]" data-test="copy-key" type="button" class="btn btn-secondary" @click="copy(key)">{{ t('governance.copyKey') }}</button>
        </template>
        <span v-else class="text-sm text-amber-700">{{ t('governance.keyPending') }}</span>
      </div>
      <p class="text-[11px] tabular-nums text-gray-400">{{ t('governance.keyCreatedAt') }} {{ formatGovernanceTime(key.created_at) }} · {{ t('governance.keyUpdatedAt') }} {{ formatGovernanceTime(key.updated_at) }}</p>
      <code v-if="secrets[key.id]" data-test="key-secret" class="block select-all break-all rounded bg-gray-100 p-3 text-sm dark:bg-dark-900">{{ secrets[key.id] }}</code>
      <p v-else-if="keyErrors[key.id]" data-test="key-read-error" role="alert" class="text-sm text-red-600">{{ t('governance.keyReadFailed') }} · {{ keyErrors[key.id] }}</p>
      <p v-else-if="key.has_key" class="text-sm text-gray-500">{{ t('governance.keyLoading') }}</p>
    </article>
    <div v-if="copyable.length" class="flex flex-wrap items-center gap-2">
      <button id="governance-copy-all-keys" data-test="copy-all-keys" type="button" class="btn btn-secondary" @click="copy()">{{ t('governance.copyAllKeys') }}</button>
      <p class="text-xs text-gray-500">{{ t('governance.transientKeys') }}</p>
    </div>
    <p v-if="copied" role="status" class="text-sm text-emerald-700">{{ t('governance.copied') }}</p>
  </section>
</template>
