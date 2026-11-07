<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type DeliveryChannel, type DeliveryRecord, type Page } from '@/api/admin/upstream-governance'
import BalanceNotificationRecords from './BalanceNotificationRecords.vue'
import StatusIndicator from './StatusIndicator.vue'
import { formatGovernanceTime } from './format'

// Every outbound notification in one place: upstream change mails, AstrBot
// group messages, and the low-balance mails sent to platform users.
type Channel = DeliveryChannel | 'users'
const props = defineProps<{ initialChannel?: Channel }>()
const { t } = useI18n()
const channel = ref<Channel>(props.initialChannel ?? 'email')
const result = ref<Page<DeliveryRecord> | null>(null)
const loading = ref(false)
const failed = ref(false)
let generation = 0

const statusTone = {
  sent: 'success',
  pending: 'warning',
  sending: 'info',
} as const
function deliveryTone(status: string) {
  return statusTone[status as keyof typeof statusTone] || 'neutral'
}
const knownStatuses = new Set(['sent', 'pending', 'sending'])
const statusLabel = (status: string) => t(`governance.deliveryRecords.status.${knownStatuses.has(status) ? status : 'unknown'}`)
const astrbotKinds = new Set(['created', 'updated', 'deleted'])
function title(record: DeliveryRecord): string {
  if (record.channel === 'email') return record.subject || record.kind
  return t(`governance.deliveryRecords.astrbotKinds.${astrbotKinds.has(record.kind) ? record.kind : 'other'}`)
}
function target(record: DeliveryRecord): string {
  if (record.channel === 'email') return record.site_name || (record.site_id ? `#${record.site_id}` : '—')
  return record.group_name || (record.group_id ? `#${record.group_id}` : '—')
}

async function load(page = 1) {
  if (channel.value === 'users') return
  const request = ++generation
  loading.value = true
  failed.value = false
  try {
    const value = await api.deliveryRecords(channel.value, page)
    if (request === generation) result.value = value
  } catch {
    if (request === generation) failed.value = true
  } finally {
    if (request === generation) loading.value = false
  }
}
watch(channel, () => { result.value = null; void load() })
onMounted(() => { void load() })
onUnmounted(() => { generation++ })
</script>
<template>
  <section data-test="delivery-records" class="min-w-0 space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700" role="group" :aria-label="t('governance.deliveryRecords.channel')">
        <button v-for="option in (['email', 'astrbot', 'users'] as const)" :key="option" type="button" :data-test="'delivery-channel-' + option" class="min-h-8 rounded-md px-3 text-sm transition-colors" :class="channel === option ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="channel === option" @click="channel = option">{{ t(`governance.deliveryRecords.channels.${option}`) }}</button>
      </div>
      <button v-if="channel !== 'users'" type="button" class="btn btn-secondary text-xs" :disabled="loading" @click="load(result?.page || 1)">{{ t('common.refresh') }}</button>
    </div>
    <BalanceNotificationRecords v-if="channel === 'users'" />
    <template v-else>
      <p v-if="failed" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-600 dark:bg-red-900/10">{{ t('governance.deliveryRecords.readFailed') }}</p>
      <p v-else-if="loading && !result" role="status" class="p-6 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-else-if="result" class="min-w-0 max-w-full overflow-x-auto rounded-xl border border-gray-200 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-dark-600" role="region" tabindex="0" :aria-label="t('governance.deliveryRecords.channels.' + channel)">
        <table class="w-full min-w-[640px] text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900">
            <tr>
              <th class="px-3 py-2 font-medium">{{ t('governance.deliveryRecords.time') }}</th>
              <th class="px-3 py-2 font-medium">{{ t(channel === 'email' ? 'governance.deliveryRecords.site' : 'governance.deliveryRecords.group') }}</th>
              <th class="px-3 py-2 font-medium">{{ t('governance.deliveryRecords.content') }}</th>
              <th v-if="channel === 'email'" class="px-3 py-2 font-medium">{{ t('governance.deliveryRecords.recipient') }}</th>
              <th class="px-3 py-2 font-medium">{{ t('governance.deliveryRecords.result') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="record in result.items" :key="record.id" data-test="delivery-record">
              <td class="whitespace-nowrap px-3 py-2 text-xs tabular-nums text-gray-500">{{ formatGovernanceTime(record.sent_at || record.created_at) }}</td>
              <td class="px-3 py-2">{{ target(record) }}</td>
              <td class="max-w-md px-3 py-2"><span class="line-clamp-2 break-words">{{ title(record) }}</span></td>
              <td v-if="channel === 'email'" class="px-3 py-2 text-xs text-gray-500">{{ record.recipient || '—' }}</td>
              <td class="px-3 py-2">
                <StatusIndicator :label="statusLabel(record.status)" :tone="deliveryTone(record.status)" />
                <span v-if="record.attempts > 0 && record.status !== 'sent'" class="ml-1 text-xs text-gray-500">{{ t('governance.deliveryRecords.attempts', { count: record.attempts }) }}</span>
                <p v-if="record.last_error" class="mt-1 break-all text-xs text-red-600 dark:text-red-400">{{ record.last_error }}</p>
              </td>
            </tr>
            <tr v-if="!result.items.length"><td :colspan="channel === 'email' ? 5 : 4" class="p-6 text-center text-sm text-gray-500">{{ t('governance.deliveryRecords.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
      <div v-if="result && result.total > result.page_size" class="flex items-center justify-end gap-3 text-xs text-gray-500">
        <span>{{ t('governance.workbench.pageSummary', { page: result.page, pages: Math.max(1, Math.ceil(result.total / result.page_size)), total: result.total }) }}</span>
        <button type="button" class="btn btn-secondary text-xs" :disabled="loading || result.page <= 1" @click="load(result.page - 1)">{{ t('governance.previous') }}</button>
        <button type="button" class="btn btn-secondary text-xs" :disabled="loading || result.page >= Math.ceil(result.total / result.page_size)" @click="load(result.page + 1)">{{ t('governance.next') }}</button>
      </div>
    </template>
  </section>
</template>
