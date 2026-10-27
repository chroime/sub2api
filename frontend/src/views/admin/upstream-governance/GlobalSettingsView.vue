<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, { type AstrBotGroup, type AstrBotGroupsInput, type GlobalNotificationEvents, type GlobalSettings } from '@/api/admin/upstream-governance'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const form = ref<GlobalSettings>(defaults())
const groups = ref<AstrBotGroup[]>([])
const groupsLoading = ref(false)
const groupsLoaded = ref(false)
const groupsError = ref('')
let groupsRequest = 0
let savedBaseUrl = ''
let applyingSettings = false
const notificationEventKeys = [
  'group_online',
  'group_offline',
  'group_name_change',
  'group_description_change',
  'group_rate_change',
] as const
const notificationEventLabels = [
  'groupOnline',
  'groupOffline',
  'groupNameChange',
  'groupDescriptionChange',
  'groupRateChange',
] as const

function defaults(): GlobalSettings {
  return {
    version: 0,
    notifications: {
      enabled: false,
      recipients: [],
      events: {
        group_online: true,
        group_offline: true,
        group_name_change: true,
        group_description_change: true,
        group_rate_change: true,
      },
    },
    local_monitor: { enabled: false, interval_seconds: 60 },
    defaults: { fast_interval_seconds: 60, full_interval_seconds: 900 },
    astrbot: { enabled: false, base_url: '', endpoint_path: '/api/v1/im/message', api_key: '', api_key_configured: false, clear_api_key: false, events: { group_online: true, group_offline: true, group_name_change: true, group_description_change: true, group_rate_change: true }, targets: [], timeout_seconds: 10 },
  }
}

function mergeEvents(value: Partial<GlobalNotificationEvents> = {}, channel: 'mail' | 'astrbot'): GlobalNotificationEvents {
  const groupDefault = channel === 'mail' ? value.upstream_group_change ?? true : value.local_group_change ?? true
  const rateDefault = channel === 'mail' ? value.upstream_rate_change ?? true : groupDefault
  return {
    ...value,
    group_online: value.group_online ?? groupDefault,
    group_offline: value.group_offline ?? groupDefault,
    group_name_change: value.group_name_change ?? groupDefault,
    group_description_change: value.group_description_change ?? groupDefault,
    group_rate_change: value.group_rate_change ?? rateDefault,
  }
}

function mergeSettings(value: Partial<GlobalSettings>): GlobalSettings {
  const initial = defaults()
  return {
    ...initial,
    ...value,
    notifications: { ...initial.notifications, ...(value.notifications || {}), events: mergeEvents(value.notifications?.events, 'mail') },
    local_monitor: { ...initial.local_monitor, ...(value.local_monitor || {}) },
    defaults: { ...initial.defaults, ...(value.defaults || {}) },
    astrbot: {
      ...initial.astrbot,
      ...(value.astrbot || {}),
      // Older servers may return the secret. Never place it back in the editable draft.
      api_key: '',
      api_key_configured: !value.astrbot?.clear_api_key && Boolean(value.astrbot?.api_key_configured || value.astrbot?.api_key),
      clear_api_key: false,
      events: mergeEvents(value.astrbot?.events, 'astrbot'),
      targets: value.astrbot?.targets ? value.astrbot.targets.map(item => ({ ...item })) : [],
    },
  }
}

function invalidateGroups() {
  groupsRequest++
  groupsLoading.value = false
  groupsLoaded.value = false
  groupsError.value = ''
  groups.value = []
}

watch(() => [form.value.astrbot.base_url, form.value.astrbot.api_key, form.value.astrbot.timeout_seconds, form.value.astrbot.clear_api_key], () => {
  if (!applyingSettings) invalidateGroups()
}, { flush: 'sync' })

function applySettings(value: GlobalSettings, preserveGroups = false) {
  groupsRequest++
  groupsLoading.value = false
  if (!preserveGroups) invalidateGroups()
  applyingSettings = true
  form.value = mergeSettings(value)
  savedBaseUrl = form.value.astrbot.base_url.trim()
  applyingSettings = false
}

function setApiKey(event: Event) {
  form.value.astrbot.api_key = (event.target as HTMLInputElement).value
  if (form.value.astrbot.api_key) form.value.astrbot.clear_api_key = false
}

function clearApiKey() {
  form.value.astrbot.api_key = ''
  form.value.astrbot.clear_api_key = true
}

function groupLabel(group: AstrBotGroup) {
  return `${group.group_name || group.group_id} · QQ ${group.group_id} · ${t('governance.smartOperations.settingsPage.qqTargetBot')} ${group.bot_id}`
}

function targetOptions(index: number): SelectOption[] {
  const target = form.value.astrbot.targets[index]
  const options: SelectOption[] = groups.value.map(group => ({
    value: group.umo,
    label: groupLabel(group),
    description: group.umo,
    disabled: form.value.astrbot.targets.some((item, otherIndex) => otherIndex !== index && item.umo === group.umo),
  }))
  if (target.umo && !groups.value.some(group => group.umo === target.umo)) {
    options.unshift({
      value: target.umo,
      label: `${target.label || target.umo} · ${t(`governance.smartOperations.settingsPage.${groupsLoaded.value ? 'qqTargetUnavailableShort' : 'qqTargetUnverified'}`)}`,
      description: target.umo,
      disabled: true,
    })
  }
  return options
}

function selectTarget(index: number, value: SelectOption['value']) {
  if (saving.value) return
  const group = groups.value.find(item => item.umo === value)
  if (!group || form.value.astrbot.targets.some((target, otherIndex) => otherIndex !== index && target.umo === group.umo)) return
  form.value.astrbot.targets[index].umo = group.umo
  form.value.astrbot.targets[index].label = group.group_name
}

function targetUnavailable(umo: string) {
  return groupsLoaded.value && Boolean(umo) && !groups.value.some(group => group.umo === umo)
}

function groupFailure(reason: unknown) {
  const messages: Record<string, string> = {
    astrbot_not_configured: 'astrbotNotConfigured',
    astrbot_auth_failed: 'astrbotAuthFailed',
    astrbot_scope_required: 'astrbotScopeRequired',
    astrbot_groups_unsupported: 'astrbotGroupsUnsupported',
    astrbot_unavailable: 'astrbotUnavailable',
    astrbot_groups_incomplete: 'astrbotGroupsIncomplete',
    astrbot_invalid_response: 'astrbotInvalidResponse',
  }
  return t(`governance.smartOperations.settingsPage.${messages[String(reason)] || 'astrbotGroupsFailed'}`)
}

async function refreshGroups() {
  if (groupsLoading.value || saving.value) return
  const config = form.value.astrbot
  const baseUrl = config.base_url.trim()
  const apiKey = config.api_key.trim() ? config.api_key : ''
  groupsError.value = ''
  if (!baseUrl || config.clear_api_key || (!apiKey && !config.api_key_configured)) {
    groupsError.value = groupFailure('astrbot_not_configured')
    return
  }
  if (!validUrl(baseUrl)) {
    groupsError.value = t('governance.smartOperations.settingsPage.invalidUrl')
    return
  }
  if (!Number.isInteger(config.timeout_seconds) || config.timeout_seconds <= 0) {
    groupsError.value = t('governance.smartOperations.settingsPage.invalidInterval')
    return
  }
  if (!apiKey && baseUrl !== savedBaseUrl) {
    groupsError.value = t('governance.smartOperations.settingsPage.astrbotChangedAddressKey')
    return
  }
  const input: AstrBotGroupsInput = { base_url: baseUrl, timeout_seconds: config.timeout_seconds }
  if (apiKey) input.api_key = apiKey
  const request = ++groupsRequest
  groupsLoading.value = true
  try {
    const result = await api.astrbotGroups(input)
    if (request !== groupsRequest) return
    if (result?.complete !== true) throw { reason: 'astrbot_groups_incomplete' }
    if (!Array.isArray(result.groups) || result.groups.some(group =>
      !group || typeof group.umo !== 'string' || !group.umo.trim() ||
      typeof group.group_id !== 'string' || !group.group_id.trim() ||
      typeof group.group_name !== 'string' || typeof group.bot_id !== 'string' || !group.bot_id.trim(),
    ) || new Set(result.groups.map(group => group.umo)).size !== result.groups.length) {
      throw { reason: 'astrbot_invalid_response' }
    }
    groups.value = result.groups
    groupsLoaded.value = true
    for (const target of form.value.astrbot.targets) {
      const group = groups.value.find(item => item.umo === target.umo)
      if (group) target.label = group.group_name
    }
  } catch (cause) {
    if (request === groupsRequest) groupsError.value = groupFailure((cause as { reason?: unknown } | null)?.reason)
  } finally {
    if (request === groupsRequest) groupsLoading.value = false
  }
}

function parseRecipients(value: string) {
  return value.split(/[\n,;]+/).map(item => item.trim()).filter(Boolean)
}

function recipientsText() {
  return form.value.notifications.recipients.join('\n')
}

function setRecipients(event: Event) {
  form.value.notifications.recipients = parseRecipients((event.target as HTMLTextAreaElement).value)
}

function addTarget() {
  form.value.astrbot.targets.push({ umo: '', label: '', enabled: true })
  if (!groupsLoaded.value) void refreshGroups()
}

function removeTarget(index: number) {
  form.value.astrbot.targets.splice(index, 1)
}

function validUrl(value: string) {
  if (!value.trim()) return true
  try { return ['http:', 'https:'].includes(new URL(value).protocol) } catch { return false }
}

function validate() {
  const intervals = [form.value.local_monitor.interval_seconds, form.value.defaults.fast_interval_seconds, form.value.defaults.full_interval_seconds, form.value.astrbot.timeout_seconds]
  if (intervals.some(value => !Number.isInteger(value) || value <= 0)) { error.value = t('governance.smartOperations.settingsPage.invalidInterval'); return false }
  if (!validUrl(form.value.astrbot.base_url)) { error.value = t('governance.smartOperations.settingsPage.invalidUrl'); return false }
  if (form.value.notifications.enabled && !form.value.notifications.recipients.length) { error.value = t('governance.smartOperations.settingsPage.invalidRecipients'); return false }
  const targets = form.value.astrbot.targets.map(target => target.umo.trim())
  if (targets.some(umo => !umo)) { error.value = t('governance.smartOperations.settingsPage.qqTargetRequired'); return false }
  if (new Set(targets).size !== targets.length) { error.value = t('governance.smartOperations.settingsPage.qqTargetDuplicate'); return false }
  return true
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    applySettings(await api.globalSettings())
    if (form.value.astrbot.base_url.trim() && form.value.astrbot.api_key_configured) void refreshGroups()
  } catch { error.value = t('governance.smartOperations.settingsPage.loadFailed') } finally { loading.value = false }
}

async function save() {
  if (saving.value || !validate()) return
  saving.value = true
  // A pending list must not update target labels while their settings are being saved.
  groupsRequest++
  groupsLoading.value = false
  error.value = ''
  saved.value = false
  try {
    const result = await api.saveGlobalSettings(form.value)
    applySettings(result, result.astrbot.base_url.trim() === form.value.astrbot.base_url.trim())
    saved.value = true
  } catch { error.value = t('governance.smartOperations.settingsPage.saveFailed') } finally { saving.value = false }
}

onMounted(load)
onUnmounted(() => { groupsRequest++ })
</script>

<template>
  <section data-test="global-settings" class="min-w-0 space-y-5">
    <header class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h3 class="text-xl font-semibold">{{ t('governance.smartOperations.settingsPage.title') }}</h3>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.description') }}</p>
      </div>
      <button type="button" class="btn btn-primary" data-test="global-settings-save" :disabled="loading || saving" @click="save"><Icon name="check" size="sm" class="mr-2" />{{ saving ? t('common.processing') : t('governance.smartOperations.settingsPage.save') }}</button>
    </header>
    <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
    <p v-if="saved" role="status" class="rounded-xl border border-green-200 bg-green-50 p-3 text-sm text-green-700 dark:border-green-900 dark:bg-green-900/20 dark:text-green-300">{{ t('governance.smartOperations.settingsPage.saved') }}</p>
    <div v-if="loading" role="status" class="rounded-xl border border-dashed border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('common.loading') }}</div>
    <form v-else @submit.prevent="save">
      <fieldset data-test="global-settings-fields" :disabled="saving" :aria-busy="saving" class="min-w-0 space-y-5 border-0 p-0">
      <section class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
        <header class="flex items-start justify-between gap-3">
          <div class="flex min-w-0 flex-1 items-start gap-3">
            <span class="shrink-0 rounded-lg bg-primary-50 p-2 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"><Icon name="mail" size="sm" /></span>
            <div class="min-w-0"><h4 id="global-email-heading" class="font-semibold">{{ t('governance.smartOperations.settingsPage.notifications') }}</h4><p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.notificationsHint') }}</p></div>
          </div>
          <label class="flex shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap text-sm"><input v-model="form.notifications.enabled" aria-describedby="global-email-heading" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />{{ t('governance.smartOperations.settingsPage.enabled') }}</label>
        </header>
        <label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.recipients') }}</span><textarea :value="recipientsText()" rows="3" class="input w-full" :disabled="!form.notifications.enabled" @input="setRecipients" /></label>
        <fieldset class="grid grid-cols-1 gap-2 sm:grid-cols-2"><legend class="mb-1 text-sm font-medium">{{ t('governance.smartOperations.settingsPage.events') }}</legend><label v-for="(key, index) in notificationEventKeys" :key="key" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="form.notifications.events[key]" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" /><span>{{ t(`governance.smartOperations.settingsPage.${notificationEventLabels[index]}`) }}</span></label></fieldset>
      </section>

      <section class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
        <header class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1"><h4 id="global-local-monitor-heading" class="font-semibold">{{ t('governance.smartOperations.settingsPage.localMonitor') }}</h4><p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.localMonitorHint') }}</p></div>
          <label class="flex shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap text-sm"><input v-model="form.local_monitor.enabled" aria-describedby="global-local-monitor-heading" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />{{ t('governance.smartOperations.settingsPage.enabled') }}</label>
        </header>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2"><label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.intervalSeconds') }}</span><input v-model.number="form.local_monitor.interval_seconds" class="input w-full" type="number" min="1" step="1" /></label></div>
      </section>

      <section class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5"><div><h4 class="font-semibold">{{ t('governance.smartOperations.settingsPage.defaults') }}</h4><p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.defaultsHint') }}</p></div><div class="grid grid-cols-1 gap-3 sm:grid-cols-2"><label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.fastIntervalSeconds') }}</span><input v-model.number="form.defaults.fast_interval_seconds" data-test="global-fast-interval" class="input w-full" type="number" min="1" step="1" /></label><label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.fullIntervalSeconds') }}</span><input v-model.number="form.defaults.full_interval_seconds" data-test="global-full-interval" class="input w-full" type="number" min="1" step="1" /></label></div></section>

      <section class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
        <header class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1"><h4 id="global-astrbot-heading" class="font-semibold">{{ t('governance.smartOperations.settingsPage.astrbot') }}</h4><p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.astrbotHint') }}</p></div>
          <label class="flex shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap text-sm"><input v-model="form.astrbot.enabled" aria-describedby="global-astrbot-heading" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />{{ t('governance.smartOperations.settingsPage.enabled') }}</label>
        </header>
        <fieldset class="grid grid-cols-1 gap-2 sm:grid-cols-2"><legend class="mb-1 text-sm font-medium">{{ t('governance.smartOperations.settingsPage.astrbotEvents') }}</legend><label v-for="(key, index) in notificationEventKeys" :key="key" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="form.astrbot.events[key]" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" /><span>{{ t(`governance.smartOperations.settingsPage.${notificationEventLabels[index]}`) }}</span></label></fieldset>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.baseUrl') }}</span><input v-model="form.astrbot.base_url" data-test="astrbot-base-url" class="input w-full" type="url" /></label>
          <label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.endpointPath') }}</span><input v-model="form.astrbot.endpoint_path" class="input w-full" /></label>
          <div class="space-y-1">
            <label class="block text-sm">
              <span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.apiKey') }}</span>
              <input :value="form.astrbot.api_key" data-test="astrbot-api-key" class="input w-full font-mono" type="password" autocomplete="new-password" :placeholder="t(`governance.smartOperations.settingsPage.${form.astrbot.api_key_configured && !form.astrbot.clear_api_key ? 'apiKeyConfigured' : 'apiKeyPlaceholder'}`)" @input="setApiKey" />
            </label>
            <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
              <p class="text-gray-500 dark:text-dark-300">{{ t(`governance.smartOperations.settingsPage.${form.astrbot.clear_api_key ? 'apiKeyClearPending' : 'apiKeyHint'}`) }}</p>
              <button v-if="(form.astrbot.api_key_configured || form.astrbot.api_key) && !form.astrbot.clear_api_key" type="button" data-test="astrbot-clear-api-key" class="text-red-600 hover:underline dark:text-red-400" @click="clearApiKey">{{ t('governance.smartOperations.settingsPage.clearApiKey') }}</button>
            </div>
          </div>
          <label class="block text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('governance.smartOperations.settingsPage.timeoutSeconds') }}</span><input v-model.number="form.astrbot.timeout_seconds" data-test="astrbot-timeout" class="input w-full" type="number" min="1" step="1" /></label>
        </div>
        <div class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h5 class="text-sm font-medium">{{ t('governance.smartOperations.settingsPage.qqTargets') }}</h5>
            <div class="flex flex-wrap gap-2">
              <button type="button" data-test="astrbot-refresh-groups" class="btn btn-secondary text-sm" :disabled="groupsLoading" @click="refreshGroups">{{ t(`governance.smartOperations.settingsPage.${groupsLoading ? 'qqGroupsLoading' : 'refreshGroups'}`) }}</button>
              <button type="button" data-test="astrbot-add-target" class="btn btn-secondary text-sm" @click="addTarget"><Icon name="plus" size="sm" class="mr-1" />{{ t('governance.smartOperations.settingsPage.addTarget') }}</button>
            </div>
          </div>
          <p class="text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.qqGroupsHint') }}</p>
          <p v-if="groupsError" data-test="astrbot-groups-error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/20 dark:text-red-300">{{ groupsError }}</p>
          <p v-else-if="groupsLoaded && !groups.length" data-test="astrbot-groups-empty" class="text-sm text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.qqGroupsEmpty') }}</p>
          <div v-for="(target, index) in form.astrbot.targets" :key="index" data-test="astrbot-target" class="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-900">
            <div class="grid grid-cols-1 items-end gap-2 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_auto_auto]">
              <div class="min-w-0">
                <label :for="`astrbot-target-${index}`" class="mb-1 block text-xs text-gray-500">{{ t('governance.smartOperations.settingsPage.qqTargetSelect') }}</label>
                <Select :id="`astrbot-target-${index}`" :model-value="target.umo" :options="targetOptions(index)" :searchable="true" :loading="groupsLoading" :disabled="groupsLoading || saving" :aria-label="t('governance.smartOperations.settingsPage.qqTargetSelect')" :placeholder="t('governance.smartOperations.settingsPage.qqTargetPlaceholder')" :search-placeholder="t('governance.smartOperations.settingsPage.qqTargetSearch')" :empty-text="t(`governance.smartOperations.settingsPage.${groupsLoaded ? 'qqGroupsNoMatch' : 'qqGroupsLoadFirst'}`)" @update:model-value="selectTarget(index, $event)" />
              </div>
              <label class="block min-w-0 text-sm"><span class="mb-1 block text-xs text-gray-500">{{ t('governance.smartOperations.settingsPage.qqTargetLabel') }}</span><input :value="target.label" data-test="astrbot-target-label" class="input w-full bg-gray-100 dark:bg-dark-800" readonly /></label>
              <label class="flex items-center gap-2 pb-2 text-sm"><input v-model="target.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />{{ t('governance.smartOperations.settingsPage.enabled') }}</label>
              <button type="button" class="btn btn-secondary text-sm" @click="removeTarget(index)">{{ t('governance.smartOperations.settingsPage.removeTarget') }}</button>
            </div>
            <p v-if="target.umo" class="break-all font-mono text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.settingsPage.qqTargetUmo') }}: {{ target.umo }}</p>
            <p v-if="targetUnavailable(target.umo)" data-test="astrbot-target-unavailable" class="text-xs text-amber-700 dark:text-amber-300">{{ t('governance.smartOperations.settingsPage.qqTargetUnavailable') }}</p>
          </div>
        </div>
      </section>

      </fieldset>
    </form>
  </section>
</template>
