<template>
  <fieldset class="rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-input-routing">
    <legend class="px-1 text-sm font-medium text-gray-900 dark:text-gray-100">
      {{ t('admin.groups.inputRouting.title') }}
    </legend>
    <div class="flex items-start justify-between gap-3">
      <p class="min-w-0 flex-1 text-sm leading-6 text-gray-500 dark:text-gray-400">
        {{ t('admin.groups.inputRouting.description') }}
      </p>
      <Toggle
        data-testid="input-routing-toggle"
        :aria-label="t('admin.groups.inputRouting.enable')"
        :model-value="modelValue.enabled"
        @update:model-value="emit('update:modelValue', { ...modelValue, enabled: $event })"
      />
    </div>
    <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
      {{ t('admin.groups.inputRouting.estimateHint') }}
    </p>
    <p class="mt-2 rounded-lg bg-gray-50 px-3 py-2 text-xs leading-5 text-gray-600 dark:bg-dark-800 dark:text-gray-300">
      {{ t('admin.groups.inputRouting.billingHint') }}
    </p>
    <p v-if="!modelValue.enabled" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.groups.inputRouting.disabledHint') }}
    </p>

    <div class="mt-4 space-y-4">
      <section
        v-for="(rule, index) in modelValue.rules"
        :key="rule.id"
        class="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 dark:border-dark-600 dark:bg-dark-800/40"
        data-testid="input-routing-rule"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.groups.inputRouting.ruleNumber', { number: index + 1 }) }}</span>
          <div class="flex items-center gap-2">
            <Toggle
              data-testid="input-routing-rule-toggle"
              :aria-label="t('admin.groups.inputRouting.enableRule')"
              :model-value="rule.enabled"
              @update:model-value="updateRule(rule.id, { enabled: $event })"
            />
            <button type="button" class="rounded p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700" :disabled="index === 0" :aria-label="t('admin.groups.inputRouting.moveUp')" data-testid="input-routing-move-up" @click="moveRule(index, -1)">
              <Icon name="chevronUp" size="sm" />
            </button>
            <button type="button" class="rounded p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700" :disabled="index === modelValue.rules.length - 1" :aria-label="t('admin.groups.inputRouting.moveDown')" data-testid="input-routing-move-down" @click="moveRule(index, 1)">
              <Icon name="chevronDown" size="sm" />
            </button>
            <button type="button" class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20" :aria-label="t('admin.groups.inputRouting.removeRule')" data-testid="input-routing-remove-rule" @click="removeRule(rule.id)">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>
        <label class="block">
          <span class="input-label">{{ t('admin.groups.inputRouting.ruleName') }}</span>
          <input :value="rule.name" type="text" maxlength="128" class="input" data-testid="input-routing-name" @input="updateRule(rule.id, { name: ($event.target as HTMLInputElement).value })" />
        </label>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label :for="`${editorId}-${rule.id}-request-type`" class="input-label">{{ t('admin.groups.inputRouting.requestType') }}</label>
            <Select
              :id="`${editorId}-${rule.id}-request-type`"
              :model-value="rule.request_type"
              :options="requestTypeOptions"
              :aria-label="t('admin.groups.inputRouting.requestType')"
              :searchable="false"
              data-testid="input-routing-request-type"
              @update:model-value="updateRequestType(rule.id, $event)"
            />
          </div>
          <div>
            <label :for="`${editorId}-${rule.id}-input-mode`" class="input-label">{{ t('admin.groups.inputRouting.inputCondition') }}</label>
            <Select
              :id="`${editorId}-${rule.id}-input-mode`"
              :model-value="rule.input_tokens_mode ?? 'range'"
              :options="inputModeOptions"
              :aria-label="t('admin.groups.inputRouting.inputCondition')"
              :searchable="false"
              data-testid="input-routing-input-mode"
              @update:model-value="updateInputMode(rule.id, $event)"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.groups.inputRouting.rangeHint') }}</p>
          </div>
          <div v-if="rule.input_tokens_mode !== 'all'" class="sm:col-span-2">
            <span class="input-label">{{ t('admin.groups.inputRouting.range') }}</span>
            <div class="flex items-center gap-2">
              <input :value="rule.min_input_tokens" type="number" min="0" :max="inputRoutingLimits.tokens" step="1" class="input min-w-0 flex-1" :aria-label="t('admin.groups.inputRouting.minTokens')" data-testid="input-routing-min-tokens" @input="updateRule(rule.id, { min_input_tokens: Number(($event.target as HTMLInputElement).value) })" />
              <span class="text-gray-400" aria-hidden="true">—</span>
              <input :value="rule.max_input_tokens" type="number" min="0" :max="inputRoutingLimits.tokens" step="1" class="input min-w-0 flex-1" :aria-label="t('admin.groups.inputRouting.maxTokens')" data-testid="input-routing-max-tokens" @input="updateRule(rule.id, { max_input_tokens: Number(($event.target as HTMLInputElement).value) })" />
            </div>
          </div>
        </div>

        <div>
          <label class="input-label" :for="`${editorId}-${rule.id}-accounts`">{{ t('admin.groups.inputRouting.accounts') }}</label>
          <div v-if="rule.account_ids.length" class="mb-2 flex flex-wrap gap-1.5" data-testid="input-routing-account-tags">
            <span v-for="id in rule.account_ids" :key="id" class="inline-flex max-w-full items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              <span class="truncate">{{ accountNames[id] || `#${id}` }}</span>
              <button type="button" :aria-label="t('admin.groups.inputRouting.removeAccount', { id })" @click="updateRule(rule.id, { account_ids: rule.account_ids.filter(value => value !== id) })"><Icon name="x" size="xs" /></button>
            </span>
          </div>
          <input
            :id="`${editorId}-${rule.id}-accounts`"
            :value="accountQueries[rule.id] || ''"
            type="search"
            class="input text-sm"
            :placeholder="t('admin.groups.inputRouting.searchAccounts')"
            data-testid="input-routing-account-search"
            @input="searchAccounts(rule.id, ($event.target as HTMLInputElement).value)"
            @focus="searchAccounts(rule.id, accountQueries[rule.id] || '')"
          />
          <div v-if="activeAccountSearch === rule.id" class="mt-1 max-h-40 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800" data-testid="input-routing-account-results">
            <p v-if="accountLoading[rule.id]" class="px-3 py-2 text-xs text-gray-500">{{ t('common.loading') }}</p>
            <p v-else-if="accountErrors[rule.id]" role="alert" class="px-3 py-2 text-xs text-red-600 dark:text-red-400">{{ t('admin.groups.inputRouting.accountsLoadError') }}</p>
            <p v-else-if="!accountResults[rule.id]?.length" class="px-3 py-2 text-xs text-gray-500">{{ t('admin.groups.inputRouting.noAccounts') }}</p>
            <button
              v-for="account in accountResults[rule.id] || []"
              :key="account.id"
              type="button"
              :disabled="rule.account_ids.includes(account.id) || rule.account_ids.length >= inputRoutingLimits.accounts"
              class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-40 dark:text-gray-200 dark:hover:bg-dark-700"
              :data-account-id="account.id"
              @click="selectAccount(rule, account)"
            >
              <span class="min-w-0 flex-1 truncate">{{ account.name }}</span>
              <span class="shrink-0 rounded border px-1.5 py-0.5 text-xs" :class="platformBadgeClass(account.platform)">{{ platformLabel(account.platform) }}</span>
              <span class="shrink-0 text-xs text-gray-400">#{{ account.id }}</span>
            </button>
          </div>
        </div>

        <div>
          <label class="input-label" :for="`${editorId}-${rule.id}-groups`">{{ t('admin.groups.inputRouting.groups') }}</label>
          <div v-if="rule.group_ids.length" class="mb-2 flex flex-wrap gap-1.5" data-testid="input-routing-group-tags">
            <span v-for="id in rule.group_ids" :key="id" class="inline-flex max-w-full items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              <span class="truncate">{{ availableGroups.find(group => group.id === id)?.name || `#${id}` }}</span>
              <button type="button" :aria-label="t('admin.groups.inputRouting.removeGroup', { id })" @click="updateRule(rule.id, { group_ids: rule.group_ids.filter(value => value !== id) })"><Icon name="x" size="xs" /></button>
            </span>
          </div>
          <input :id="`${editorId}-${rule.id}-groups`" v-model="groupQueries[rule.id]" type="search" class="input text-sm" :placeholder="t('admin.groups.inputRouting.searchGroups')" data-testid="input-routing-group-search" />
          <div class="mt-1 grid max-h-36 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-800 sm:grid-cols-2" data-testid="input-routing-group-options">
            <p v-if="groupsLoading" class="px-2 py-1 text-xs text-gray-500">{{ t('common.loading') }}</p>
            <div v-else-if="groupsError" class="col-span-full flex items-center justify-between gap-2 px-2 py-1 text-xs text-red-600 dark:text-red-400" role="alert">
              {{ t('admin.groups.inputRouting.groupsLoadError') }}
              <button type="button" class="underline" @click="loadGroups">{{ t('common.retry') }}</button>
            </div>
            <p v-else-if="!filteredGroups(rule.id).length" class="col-span-full px-2 py-1 text-xs text-gray-500">{{ t('admin.groups.inputRouting.noGroups') }}</p>
            <label v-for="group in filteredGroups(rule.id)" :key="group.id" class="flex min-w-0 cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700">
              <input type="checkbox" :checked="rule.group_ids.includes(group.id)" :disabled="!rule.group_ids.includes(group.id) && rule.group_ids.length >= inputRoutingLimits.groups" :data-group-id="group.id" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" @change="toggleGroup(rule, group.id, ($event.target as HTMLInputElement).checked)" />
              <span class="min-w-0 flex-1 truncate">{{ group.name }}</span>
              <span class="shrink-0 rounded border px-1.5 py-0.5 text-xs" :class="platformBadgeClass(group.platform)">{{ platformLabel(group.platform) }}</span>
              <span class="shrink-0 text-xs text-gray-400">#{{ group.id }}</span>
            </label>
          </div>
          <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.groups.inputRouting.targetsHint') }}</p>
        </div>

        <div class="flex items-start justify-between gap-3 border-t border-gray-200 pt-3 dark:border-dark-600">
          <div class="min-w-0 flex-1">
            <p class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.groups.inputRouting.fallback') }}</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.groups.inputRouting.fallbackHint') }}</p>
          </div>
          <Toggle data-testid="input-routing-fallback" :aria-label="t('admin.groups.inputRouting.fallback')" :model-value="rule.fallback_to_original" @update:model-value="updateRule(rule.id, { fallback_to_original: $event })" />
        </div>
        <p v-if="showValidation && ruleError(rule)" class="text-xs text-red-600 dark:text-red-400" role="alert" data-testid="input-routing-validation-error">
          {{ t(`admin.groups.inputRouting.${ruleError(rule)}`) }}
        </p>
      </section>
    </div>
    <button type="button" class="btn btn-secondary mt-3" :disabled="modelValue.rules.length >= inputRoutingLimits.rules" data-testid="input-routing-add-rule" @click="addRule">
      <Icon name="plus" size="sm" class="mr-2" />{{ t('admin.groups.inputRouting.addRule') }}
    </button>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useKeyedDebouncedSearch } from '@/composables/useKeyedDebouncedSearch'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import type { AdminGroup, AccountListItem, GroupInputRoutingConfig, GroupInputRoutingRule, GroupPlatform } from '@/types'
import { inputRoutingLimits, inputRoutingRuleError } from './groupInputRouting'

const props = defineProps<{ modelValue: GroupInputRoutingConfig; platform: GroupPlatform; groupId?: number }>()
const emit = defineEmits<{ 'update:modelValue': [value: GroupInputRoutingConfig] }>()
const { t } = useI18n()
const requestTypeOptions = computed(() => ['all', 'sync', 'stream'].map(value => ({ value, label: t(`admin.groups.inputRouting.${value}`) })))
const inputModeOptions = computed(() => [
  { value: 'all', label: t('admin.groups.inputRouting.inputAll') },
  { value: 'range', label: t('admin.groups.inputRouting.tokenRange') },
])
const editorId = useId()
const showValidation = ref(false)
const availableGroups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
const groupsError = ref(false)
const groupQueries = reactive<Record<string, string>>({})
const accountQueries = reactive<Record<string, string>>({})
const accountNames = reactive<Record<number, string>>({})
const accountResults = reactive<Record<string, AccountListItem[]>>({})
const accountErrors = reactive<Record<string, boolean>>({})
const accountLoading = reactive<Record<string, boolean>>({})
const activeAccountSearch = ref<string | null>(null)
let disposed = false

const eligibleGroups = computed(() => availableGroups.value.filter(group => group.id !== props.groupId && group.status === 'active'))
const filteredGroups = (ruleId: string) => {
  const query = (groupQueries[ruleId] || '').trim().toLowerCase()
  return eligibleGroups.value.filter(group => !query || group.name.toLowerCase().includes(query) || String(group.id).includes(query))
}

async function loadGroups() {
  groupsLoading.value = true
  groupsError.value = false
  try {
    const groups = await adminAPI.groups.getAll()
    if (!disposed) availableGroups.value = groups ?? []
  } catch {
    if (!disposed) groupsError.value = true
  } finally {
    if (!disposed) groupsLoading.value = false
  }
}
onMounted(loadGroups)
onUnmounted(() => { disposed = true })

// IDs stay visible when a target was deleted or is unavailable; never silently drop a saved target.
const resolvedAccountIDs = new Set<number>()
watch(() => props.modelValue.rules.flatMap(rule => rule.account_ids), async ids => {
  await Promise.allSettled([...new Set(ids)].filter(id => !resolvedAccountIDs.has(id)).map(async id => {
    resolvedAccountIDs.add(id)
    const account = await adminAPI.accounts.getById(id)
    if (!disposed && account) {
      accountNames[id] = account.name
    }
  }))
}, { immediate: true })

const searchRunner = useKeyedDebouncedSearch<AccountListItem[]>({
  search: async (keyword, { signal }) => {
    const response = await adminAPI.accounts.list(1, 30, {
      search: keyword,
      status: 'active',
    }, { signal })
    return (response?.items ?? []).filter(account => account.status === 'active')
  },
  onSuccess: (id, result) => { accountResults[id] = result; accountLoading[id] = false },
  onError: id => { accountResults[id] = []; accountErrors[id] = true; accountLoading[id] = false },
})

function searchAccounts(id: string, query: string) {
  accountQueries[id] = query
  activeAccountSearch.value = id
  accountLoading[id] = true
  accountErrors[id] = false
  accountResults[id] = []
  searchRunner.trigger(id, query.trim())
}
function updateRule(id: string, patch: Partial<GroupInputRoutingRule>) {
  emit('update:modelValue', { ...props.modelValue, rules: props.modelValue.rules.map(rule => rule.id === id ? { ...rule, ...patch } : rule) })
}
function updateRequestType(id: string, value: string | number | boolean | null) {
  if (value === 'all' || value === 'sync' || value === 'stream') updateRule(id, { request_type: value })
}
function updateInputMode(id: string, value: string | number | boolean | null) {
  if (value === 'all' || value === 'range') updateRule(id, { input_tokens_mode: value })
}
function addRule() {
  if (props.modelValue.rules.length >= inputRoutingLimits.rules) return
  emit('update:modelValue', {
    ...props.modelValue,
    rules: [...props.modelValue.rules, {
      id: globalThis.crypto?.randomUUID?.() ?? `rule-${Date.now()}-${Math.random().toString(36).slice(2)}`,
      name: '', enabled: false, request_type: 'all', input_tokens_mode: 'all', min_input_tokens: 0, max_input_tokens: 0,
      account_ids: [], group_ids: [], fallback_to_original: false,
    }],
  })
}
function removeRule(id: string) {
  searchRunner.clearKey(id)
  emit('update:modelValue', { ...props.modelValue, rules: props.modelValue.rules.filter(rule => rule.id !== id) })
}
function moveRule(index: number, direction: number) {
  const destination = index + direction
  if (destination < 0 || destination >= props.modelValue.rules.length) return
  const rules = [...props.modelValue.rules]
  const [rule] = rules.splice(index, 1)
  rules.splice(destination, 0, rule!)
  emit('update:modelValue', { ...props.modelValue, rules })
}
function selectAccount(rule: GroupInputRoutingRule, account: AccountListItem) {
  if (rule.account_ids.includes(account.id) || rule.account_ids.length >= inputRoutingLimits.accounts) return
  accountNames[account.id] = account.name
  resolvedAccountIDs.add(account.id)
  updateRule(rule.id, { account_ids: [...rule.account_ids, account.id] })
}
function toggleGroup(rule: GroupInputRoutingRule, id: number, selected: boolean) {
  updateRule(rule.id, { group_ids: selected ? [...new Set([...rule.group_ids, id])] : rule.group_ids.filter(value => value !== id) })
}
function ruleError(rule: GroupInputRoutingRule): string | null {
  if (rule.group_ids.includes(props.groupId ?? 0)) return 'selfTargetError'
  return inputRoutingRuleError(rule)
}
function validate(): boolean {
  showValidation.value = true
  return props.modelValue.rules.every(rule => !ruleError(rule))
}
defineExpose({ validate })
</script>
