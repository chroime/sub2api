<template>
  <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" :aria-busy="loading || saving">
    <div>
      <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.balancePrecharge.groupTitle') }}</h4>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.balancePrecharge.groupHint') }}</p>
    </div>
    <div>
      <label :for="`group-precharge-mode-${groupId}`" class="input-label">{{ t('admin.settings.balancePrecharge.mode') }}</label>
      <select :id="`group-precharge-mode-${groupId}`" v-model="mode" class="input" :disabled="busy" data-testid="group-precharge-mode">
        <option value="inherit">{{ t('admin.settings.balancePrecharge.inherit') }}</option>
        <option value="custom">{{ t('admin.settings.balancePrecharge.custom') }}</option>
      </select>
    </div>
    <div v-if="confirmed" class="space-y-1 rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
      <p data-testid="group-precharge-effective">{{ t('admin.settings.balancePrecharge.effective', { threshold: formatBalancePrechargeMoney(confirmed.effective.threshold), amount: formatBalancePrechargeMoney(confirmed.effective.amount) }) }}</p>
      <p v-if="!confirmed.global.enabled">{{ t('admin.settings.balancePrecharge.globalDisabled') }}</p>
    </div>
    <div class="grid gap-3 sm:grid-cols-2">
      <div>
        <label :for="`group-precharge-threshold-${groupId}`" class="input-label">{{ t('admin.settings.balancePrecharge.threshold') }}</label>
        <input :id="`group-precharge-threshold-${groupId}`" v-model="threshold" type="text" inputmode="decimal" class="input" :disabled="busy || mode === 'inherit'" data-testid="group-precharge-threshold" @keydown.enter.prevent="save" />
      </div>
      <div>
        <label :for="`group-precharge-amount-${groupId}`" class="input-label">{{ t('admin.settings.balancePrecharge.amount') }}</label>
        <input :id="`group-precharge-amount-${groupId}`" v-model="amount" type="text" inputmode="decimal" class="input" :disabled="busy || mode === 'inherit'" data-testid="group-precharge-amount" @keydown.enter.prevent="save" />
      </div>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.balancePrecharge.overdraft') }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.balancePrecharge.scope') }}</p>
    <p v-if="loaded && !valid" class="text-xs text-red-600 dark:text-red-400">{{ t('admin.settings.balancePrecharge.invalidAmounts') }}</p>
    <div class="flex flex-wrap items-center gap-3">
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !valid" data-testid="group-precharge-save" @click="save">{{ t(saving ? 'admin.settings.saving' : 'admin.settings.balancePrecharge.saveGroup') }}</button>
      <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(error) }}</p>
      <p v-else-if="saved" role="status" class="text-xs text-green-600 dark:text-green-400">{{ t('admin.settings.balancePrecharge.saved') }}</p>
      <p v-else-if="loading" role="status" class="text-xs text-gray-500">{{ t('common.loading') }}</p>
      <button v-if="error" type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving" data-testid="group-precharge-retry" @click="load">{{ t('admin.settings.balancePrecharge.reload') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getGroupBalancePrechargeSettings, updateGroupBalancePrechargeSettings } from '@/api/admin/settings'
import type { GroupBalancePrechargeSettingsResponse } from '@/api/admin/settings'
import { formatBalancePrechargeMoney, parseBalancePrechargeMoney, validBalancePrechargeAmounts } from '@/utils/balancePrecharge'

const props = defineProps<{ groupId: number }>()
const { t } = useI18n()
const mode = ref<'inherit' | 'custom'>('inherit')
const threshold = ref('0')
const amount = ref('0')
const confirmed = ref<GroupBalancePrechargeSettingsResponse | null>(null)
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const saved = ref(false)
const error = ref('')
let generation = 0
const busy = computed(() => !loaded.value || loading.value || saving.value)
const valid = computed(() => mode.value === 'inherit' || validBalancePrechargeAmounts(parseBalancePrechargeMoney(threshold.value), parseBalancePrechargeMoney(amount.value), true))

function apply(value: GroupBalancePrechargeSettingsResponse) {
  mode.value = value.settings.mode
  const amounts = value.settings.mode === 'custom' ? value.settings : value.global
  threshold.value = formatBalancePrechargeMoney(amounts.threshold)
  amount.value = formatBalancePrechargeMoney(amounts.amount)
}

async function load() {
  const requestGeneration = ++generation
  const groupId = props.groupId
  loading.value = true
  loaded.value = false
  saving.value = false
  saved.value = false
  confirmed.value = null
  error.value = ''
  try {
    const value = await getGroupBalancePrechargeSettings(groupId)
    if (requestGeneration !== generation) return
    confirmed.value = value
    apply(value)
    loaded.value = true
  } catch {
    if (requestGeneration === generation) error.value = 'admin.settings.balancePrecharge.loadFailed'
  } finally {
    if (requestGeneration === generation) loading.value = false
  }
}

async function save() {
  if (busy.value || !valid.value) return
  const requestGeneration = generation
  const groupId = props.groupId
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    const value = await updateGroupBalancePrechargeSettings(groupId, {
      mode: mode.value,
      threshold: mode.value === 'inherit' ? 0 : Number(threshold.value),
      amount: mode.value === 'inherit' ? 0 : Number(amount.value),
    })
    if (requestGeneration !== generation) return
    confirmed.value = value
    apply(value)
    saved.value = true
  } catch {
    if (requestGeneration !== generation) return
    if (confirmed.value) apply(confirmed.value)
    loaded.value = false
    error.value = 'admin.settings.balancePrecharge.saveFailed'
  } finally {
    if (requestGeneration === generation) saving.value = false
  }
}

watch([mode, threshold, amount], () => { saved.value = false }, { flush: 'sync' })
watch(mode, (value) => {
  if (value === 'inherit' && confirmed.value) {
    threshold.value = formatBalancePrechargeMoney(confirmed.value.global.threshold)
    amount.value = formatBalancePrechargeMoney(confirmed.value.global.amount)
  }
}, { flush: 'sync' })
watch(() => props.groupId, () => { void load() }, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
