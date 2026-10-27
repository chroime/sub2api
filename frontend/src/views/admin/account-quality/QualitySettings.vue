<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type Settings } from '@/api/admin/account-quality'
import governanceAPI, { type AstrBotGroup } from '@/api/admin/upstream-governance'
import { qualityError } from './quality'

const props = defineProps<{ settings: Settings }>()
const emit = defineEmits<{ saved: [Settings]; busy: [boolean] }>()
const { t } = useI18n()
const draft = ref<Settings>(JSON.parse(JSON.stringify(props.settings))), saving = ref(false), error = ref(''), saved = ref(false)
const groups = ref<AstrBotGroup[]>([]), groupsLoading = ref(false), groupsLoaded = ref(false), groupsError = ref('')
const events = ['suspected', 'quarantined', 'recovered', 'manual', 'budget_paused']
const listText = (values: string[]) => values.join('\n')
const recipients = computed({ get: () => listText(draft.value.notifications.email_recipients), set: value => { draft.value.notifications.email_recipients = value.split(/[\n,，]+/).map(item => item.trim()).filter(Boolean) } })
const targetOptions = computed(() => {
  const options = groups.value.map(group => ({ umo: group.umo, label: `${group.group_name || group.group_id} · QQ ${group.group_id}` }))
  for (const target of draft.value.notifications.qq_targets) if (!options.some(item => item.umo === target)) options.push({ umo: target, label: target })
  return options
})
async function loadGroups() {
  if (groupsLoading.value) return
  groupsLoading.value = true; groupsError.value = ''
  try {
    const global = await governanceAPI.globalSettings()
    if (!global.astrbot.base_url) { groupsError.value = t('accountQuality.qqNotConfigured'); return }
    const result = await governanceAPI.astrbotGroups({ base_url: global.astrbot.base_url, timeout_seconds: global.astrbot.timeout_seconds })
    groups.value = result.groups || []; groupsLoaded.value = true
  } catch { groupsError.value = t('accountQuality.qqGroupsFailed') }
  finally { groupsLoading.value = false }
}
async function save() {
  if (saving.value) return
  const value = draft.value, notifications = value.notifications
  if (![value.concurrency, value.daily_request_limit, notifications.reminder_seconds].every(item => Number.isSafeInteger(item) && item >= 1) || value.concurrency > 20 || value.daily_request_limit > 100000 || notifications.reminder_seconds < 60 || notifications.reminder_seconds > 604800 || value.daily_cost_limit_usd < 0 || !Number.isFinite(value.daily_cost_limit_usd) || (notifications.email_enabled && (!notifications.email_recipients.length || notifications.email_recipients.some(email => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)))) || notifications.qq_enabled && !notifications.qq_targets.length) { error.value = t('accountQuality.invalidSettings'); return }
  saving.value = true; saved.value = false; error.value = ''; emit('busy', true)
  try { draft.value = await api.saveSettings(value); saved.value = true; emit('saved', draft.value) }
  catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { saving.value = false; emit('busy', false) }
}
</script>

<template>
  <form class="space-y-5" data-test="quality-settings" @submit.prevent="save">
    <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="font-semibold">{{ t('accountQuality.settings') }}</h3><button type="submit" data-test="quality-settings-save" class="btn btn-primary btn-sm" :disabled="saving">{{ t('accountQuality.save') }}</button></div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="saved" role="status" class="text-sm text-emerald-600">{{ t('accountQuality.saved') }}</p>
    <section class="grid gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:grid-cols-3">
      <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.concurrency') }}</span><input v-model.number="draft.concurrency" class="input w-full" type="number" min="1" max="20" step="1" required /></label>
      <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.globalDailyLimit') }}</span><input v-model.number="draft.daily_request_limit" class="input w-full" type="number" min="1" step="1" required /></label>
      <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.dailyCost') }}</span><input v-model.number="draft.daily_cost_limit_usd" class="input w-full" type="number" min="0" step="0.01" required /></label>
      <p class="text-xs text-gray-500 sm:col-span-3">{{ t('accountQuality.costHint') }}</p>
    </section>
    <p class="text-sm leading-relaxed text-gray-500">{{ t('accountQuality.notificationHint') }}</p>
    <div class="grid gap-4 lg:grid-cols-2">
      <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><label class="flex items-center gap-2 text-sm font-semibold"><input v-model="draft.notifications.email_enabled" data-test="quality-email-enabled" type="checkbox" class="rounded" />{{ t('accountQuality.emailEnabled') }}</label><label class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.recipients') }}</span><textarea v-model="recipients" class="input w-full" rows="3" :disabled="!draft.notifications.email_enabled" /></label></section>
      <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><label class="flex items-center gap-2 text-sm font-semibold"><input v-model="draft.notifications.qq_enabled" data-test="quality-qq-enabled" type="checkbox" class="rounded" />{{ t('accountQuality.qqEnabled') }}</label><div class="flex items-center justify-between gap-2"><span class="text-sm">{{ t('accountQuality.qqTargets') }}</span><button type="button" class="btn btn-secondary btn-sm" :disabled="groupsLoading || !draft.notifications.qq_enabled" @click="loadGroups">{{ t('accountQuality.chooseQQGroups') }}</button></div><div v-if="targetOptions.length" class="max-h-48 space-y-2 overflow-y-auto"><label v-for="target in targetOptions" :key="target.umo" class="flex items-start gap-2 text-sm"><input v-model="draft.notifications.qq_targets" :value="target.umo" type="checkbox" class="mt-1 rounded" :disabled="!draft.notifications.qq_enabled" /><span class="break-all">{{ target.label }}</span></label></div><p v-else-if="groupsLoaded" class="text-xs text-gray-500">{{ t('accountQuality.qqGroupsEmpty') }}</p><p v-if="groupsError" role="alert" class="text-xs text-amber-600">{{ groupsError }}</p><p class="text-xs leading-relaxed text-gray-500">{{ t('accountQuality.qqHint') }}</p></section>
    </div>
    <fieldset class="flex flex-wrap gap-4"><legend class="mb-2 text-sm font-semibold">{{ t('accountQuality.events') }}</legend><label v-for="event in events" :key="event" class="flex items-center gap-2 text-sm"><input v-model="draft.notifications.events" :value="event" type="checkbox" class="rounded" />{{ t(`accountQuality.event_${event}`) }}</label></fieldset>
    <label class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.reminder') }}</span><input v-model.number="draft.notifications.reminder_seconds" class="input w-full sm:w-60" type="number" min="60" max="604800" step="1" required /></label>
  </form>
</template>
