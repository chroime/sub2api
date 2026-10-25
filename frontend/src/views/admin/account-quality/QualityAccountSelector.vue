<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import accountsAPI from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'

type AccountSummary = Pick<AccountListItem, 'id' | 'name' | 'platform' | 'type' | 'status'>
const props = withDefaults(defineProps<{ modelValue: number[]; platform: string; multiple?: boolean; disabled?: boolean }>(), { multiple: false, disabled: false })
const emit = defineEmits<{
  'update:modelValue': [number[]]
  validation: [{ valid: boolean; pending: boolean; platform: string; accountIds: number[] }]
}>()
const { t } = useI18n()
const selectedIds = computed(() => [...new Set(props.modelValue.filter(id => Number.isSafeInteger(id) && id > 0))])
const cache = ref<Record<number, AccountSummary>>({}), results = ref<AccountSummary[]>([]), query = ref('')
const loading = ref(false), hydrating = ref(false), searchError = ref(false), hydrationError = ref(false)
let searchVersion = 0, hydrationVersion = 0
let searchAbort: AbortController | undefined, hydrationAbort: AbortController | undefined
const filters = { lite: '1', include_scheduler_score: '0' } as const
const unresolvedIds = computed(() => selectedIds.value.filter(id => !cache.value[id]))
const invalidIds = computed(() => selectedIds.value.filter(id => !cache.value[id] || cache.value[id]?.platform !== props.platform))
const valid = computed(() => Boolean(props.platform && selectedIds.value.length && (props.multiple || selectedIds.value.length === 1) && !invalidIds.value.length))
const canRetry = computed(() => searchError.value || hydrationError.value || !hydrating.value && unresolvedIds.value.length > 0)

function summary(account: AccountListItem): AccountSummary { return { id: account.id, name: account.name, platform: account.platform, type: account.type, status: account.status } }
function remember(accounts: AccountListItem[]) {
  const next = { ...cache.value }
  for (const account of accounts) next[account.id] = summary(account)
  cache.value = next
}
function statusLabel(status: string) {
  const key = `admin.accounts.status.${status}`, label = t(key)
  return label === key ? status : label
}
function accountLabel(id: number) {
  const account = cache.value[id]
  return account ? `${account.name} · #${account.id} · ${account.platform} · ${account.type} · ${statusLabel(account.status)}` : `#${id} · ${t(hydrating.value ? 'accountQuality.accountSelectionLoading' : 'accountQuality.accountUnavailable')}`
}
function option(id: number, disabled = false): SelectOption {
  const account = cache.value[id]
  return { value: id, label: accountLabel(id), disabled, accountName: account?.name || `#${id}`, details: account ? `#${id} · ${account.platform} · ${account.type} · ${statusLabel(account.status)}` : t('accountQuality.accountUnavailable') }
}
const options = computed(() => {
  const selected = new Set(selectedIds.value)
  const items = results.value.filter(account => account.platform === props.platform && (!props.multiple || !selected.has(account.id))).map(account => option(account.id))
  if (!props.multiple && selectedIds.value[0] && !items.some(item => item.value === selectedIds.value[0])) {
    const id = selectedIds.value[0]
    items.unshift(option(id, !cache.value[id] || cache.value[id]?.platform !== props.platform))
  }
  return items
})
const pickerValue = computed(() => props.multiple ? null : selectedIds.value[0] || null)

async function loadAccounts(search = query.value) {
  const version = ++searchVersion, platform = props.platform
  searchAbort?.abort(); searchAbort = new AbortController()
  query.value = search.trim(); results.value = []; searchError.value = false
  if (!platform) { loading.value = false; return }
  loading.value = true
  try {
    const response = await accountsAPI.list(1, 50, { ...filters, platform, ...(query.value ? { search: query.value } : {}) }, { signal: searchAbort.signal })
    if (version !== searchVersion || platform !== props.platform) return
    remember(response.items)
    results.value = response.items.filter(account => account.platform === platform).map(summary)
  } catch { if (version === searchVersion) searchError.value = true }
  finally { if (version === searchVersion) loading.value = false }
}
async function hydrateSelection() {
  const version = ++hydrationVersion
  hydrationAbort?.abort(); hydrationAbort = new AbortController()
  hydrationError.value = false
  const wanted = new Set(unresolvedIds.value)
  if (!wanted.size) { hydrating.value = false; return }
  hydrating.value = true
  try {
    for (let page = 1; wanted.size; page++) {
      const response = await accountsAPI.list(page, 1000, filters, { signal: hydrationAbort.signal })
      if (version !== hydrationVersion) return
      const found = response.items.filter(account => wanted.has(account.id))
      remember(found)
      found.forEach(account => wanted.delete(account.id))
      const pageSize = response.page_size || 1000
      if (!response.items.length || page * pageSize >= response.total) break
    }
  } catch { if (version === hydrationVersion) hydrationError.value = true }
  finally { if (version === hydrationVersion) hydrating.value = false }
}
function choose(value: SelectOption['value']) {
  if (props.disabled) return
  if (value === null) { if (!props.multiple) emit('update:modelValue', []); return }
  const id = Number(value), account = cache.value[id]
  if (!account || account.platform !== props.platform || selectedIds.value.includes(id) && props.multiple) return
  emit('update:modelValue', props.multiple ? [...selectedIds.value, id] : [id])
  if (props.multiple) void loadAccounts('')
}
function remove(id: number) { if (!props.disabled) emit('update:modelValue', selectedIds.value.filter(value => value !== id)) }
async function retry() { await Promise.all([loadAccounts(), hydrateSelection()]) }

watch(() => props.platform, () => { query.value = ''; void loadAccounts('') }, { immediate: true })
watch(() => selectedIds.value.join(','), hydrateSelection, { immediate: true })
watch([valid, hydrating, () => props.platform, () => selectedIds.value.join(',')], () => emit('validation', { valid: valid.value, pending: hydrating.value, platform: props.platform, accountIds: [...selectedIds.value] }), { immediate: true })
onUnmounted(() => { searchVersion++; hydrationVersion++; searchAbort?.abort(); hydrationAbort?.abort() })
</script>

<template>
  <div class="space-y-2" data-test="quality-target-accounts">
    <div v-if="multiple && selectedIds.length" class="flex flex-wrap gap-2">
      <span v-for="id in selectedIds" :key="id" :data-test="`quality-account-chip-${id}`" class="inline-flex max-w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs" :class="invalidIds.includes(id) ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200'">
        <span class="min-w-0 truncate" :title="accountLabel(id)">{{ accountLabel(id) }}</span>
        <button type="button" :data-test="`quality-remove-account-${id}`" class="shrink-0 rounded p-0.5 hover:text-red-600" :disabled="disabled" :aria-label="`${t('accountQuality.removeAccount')} ${cache[id]?.name || '#' + id}`" @click="remove(id)"><Icon name="x" size="xs" /></button>
      </span>
    </div>
    <Select data-test="quality-account-picker" :model-value="pickerValue" :options="options" :disabled="disabled || !platform" :loading="loading" :clearable="!multiple" remote :aria-label="t(multiple ? 'accountQuality.addAccount' : 'accountQuality.singleAccount')" :placeholder="t(multiple ? 'accountQuality.addAccount' : 'accountQuality.chooseAccount')" :search-placeholder="t('accountQuality.searchAccount')" :empty-text="t(searchError ? 'accountQuality.accountsLoadFailed' : 'accountQuality.accountsEmpty')" @search="loadAccounts" @update:model-value="choose">
      <template #option="{ option: item, selected }"><div class="min-w-0 text-left"><p class="truncate font-medium">{{ item.accountName }}</p><p class="mt-0.5 truncate text-xs text-gray-500">{{ item.details }}</p></div><Icon v-if="selected" name="check" size="sm" class="shrink-0 text-primary-500" /></template>
    </Select>
    <p v-if="loading || hydrating" role="status" class="text-xs text-gray-500">{{ t(hydrating ? 'accountQuality.accountSelectionLoading' : 'accountQuality.accountsLoading') }}</p>
    <p v-if="invalidIds.length && !hydrating" data-test="quality-account-selection-warning" class="text-xs leading-relaxed text-amber-700 dark:text-amber-300">{{ t('accountQuality.accountSelectionInvalid') }} {{ invalidIds.map(id => '#' + id).join(', ') }}</p>
    <div v-if="canRetry" class="flex flex-wrap items-center justify-between gap-2"><p v-if="searchError || hydrationError" role="alert" class="text-xs text-red-600">{{ t('accountQuality.accountsLoadFailed') }}</p><button type="button" data-test="quality-accounts-retry" class="text-xs font-medium text-primary-600 hover:underline disabled:opacity-40" :disabled="loading || hydrating || disabled" @click="retry">{{ t('accountQuality.retryAccounts') }}</button></div>
  </div>
</template>
