<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import api, { type BalanceNotification, type BalanceNotificationDetail, type BalanceNotificationPage, type BalanceNotificationQuery, type BalanceNotificationStatus, type BalanceNotificationTrigger } from '@/api/admin/balance-notifications'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatGovernanceTime } from './format'

const { t } = useI18n()
const tr = (key: string, params?: Record<string, string | number>) => t(`governance.balanceNotifications.${key}`, params || {})
const filters = reactive({ status: '', trigger_type: '', user_id: '', user_email: '', recipient: '', from: '', to: '' })
const result = ref<BalanceNotificationPage | null>(null)
const loading = ref(false), error = ref('')
const currentTime = ref(Date.now())
let statusTimer: ReturnType<typeof setInterval> | undefined
const detail = ref<BalanceNotificationDetail | null>(null), detailId = ref<number | null>(null), detailLoading = ref(false), detailError = ref('')
let generation = 0, detailGeneration = 0
let applied: BalanceNotificationQuery = { page_size: 20 }
const statusValues: BalanceNotificationStatus[] = ['pending', 'sending', 'sent', 'failed', 'suppressed']
const statuses = computed(() => [{ value: '', label: tr('all') }, ...statusValues.map(value => ({ value, label: tr(`status_${value}`) }))])
const triggers = computed(() => [{ value: '', label: tr('all') }, ...(['automatic', 'manual_batch'] as const).map(value => ({ value, label: tr(value) }))])
const columns = computed(() => [
  { key: 'user', label: tr('user') }, { key: 'recipient', label: tr('recipient') },
  { key: 'amount', label: tr('amount') }, { key: 'trigger', label: tr('trigger') },
  { key: 'result', label: tr('result') }, { key: 'time', label: tr('time') }, { key: 'actions', label: '' },
])
const summary = computed(() => (['total', 'pending', 'sent', 'failed', 'suppressed'] as const).map(key => ({ key, count: result.value?.summary[key] ?? 0 })))
function money(value: number | null | undefined) { return typeof value === 'number' && Number.isFinite(value) ? `$${value.toLocaleString('en-US', { maximumSignificantDigits: 15 })}` : '—' }
function unconfirmed(row: BalanceNotification) { return ['pending', 'sending'].includes(row.status) && currentTime.value - new Date(row.updated_at).getTime() > 120_000 }
function status(row: BalanceNotification) { return tr(unconfirmed(row) ? 'unknown' : `status_${row.status}`) }
function tone(row: BalanceNotification) {
  if (row.status === 'sent') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  if (row.status === 'failed') return 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}
const reasonKeys = new Set(['duplicate', 'unsubscribed', 'no_recipient', 'invalid_email', 'balance_above_threshold', 'balance_threshold_not_configured', 'user_not_found', 'notification_suppressed'])
function reason(row: BalanceNotification) { return reasonKeys.has(row.suppression_reason) ? tr(`reasons.${row.suppression_reason}`) : row.last_error || row.suppression_reason }
function dateQuery(value: string) { return value ? new Date(`${value}+08:00`).toISOString() : undefined }
async function load(page = result.value?.page || 1) {
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const value = await api.list({ ...applied, page })
    if (current === generation) result.value = value
  } catch { if (current === generation) error.value = tr('loadFailed') }
  finally { if (current === generation) loading.value = false }
}
async function applyFilters() {
  if (filters.user_id && (!/^[1-9]\d*$/.test(filters.user_id) || !Number.isSafeInteger(Number(filters.user_id)))) { error.value = tr('invalidUser'); return }
  let from: string | undefined, to: string | undefined
  try { from = dateQuery(filters.from); to = dateQuery(filters.to) } catch { error.value = tr('invalidRange'); return }
  if (from && to && from >= to) { error.value = tr('invalidRange'); return }
  applied = { page_size: 20, user_id: filters.user_id ? Number(filters.user_id) : undefined, user_email: filters.user_email.trim() || undefined, recipient: filters.recipient.trim() || undefined, status: filters.status as BalanceNotificationStatus || undefined, trigger_type: filters.trigger_type as BalanceNotificationTrigger || undefined, from, to }
  await load(1)
}
async function reset() { Object.assign(filters, { status: '', trigger_type: '', user_id: '', user_email: '', recipient: '', from: '', to: '' }); await applyFilters() }
async function openDetail(id: number) {
  const current = ++detailGeneration
  detailId.value = id; detail.value = null; detailLoading.value = true; detailError.value = ''
  try { const value = await api.detail(id); if (current === detailGeneration) detail.value = value }
  catch { if (current === detailGeneration) detailError.value = tr('detailFailed') }
  finally { if (current === detailGeneration) detailLoading.value = false }
}
function closeDetail() { detailGeneration++; detailId.value = null; detail.value = null; detailLoading.value = false; detailError.value = '' }
const preview = computed(() => {
  if (!detail.value?.body) return ''
  const clean = DOMPurify.sanitize(detail.value.body, { WHOLE_DOCUMENT: true, ADD_TAGS: ['style'], FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'base', 'meta', 'link', 'svg', 'math', 'video', 'audio', 'source'], FORBID_ATTR: ['href', 'src', 'srcset', 'action', 'formaction', 'poster', 'xlink:href'] })
  const policy = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'; base-uri 'none'; form-action 'none'">`
  return `<!doctype html><html><head><meta charset="utf-8">${policy}<style>body{overflow-wrap:anywhere;margin:0}img{max-width:100%}</style></head><body>${clean}</body></html>`
})
onMounted(() => { void load(1); statusTimer = setInterval(() => { currentTime.value = Date.now() }, 30_000) })
onUnmounted(() => { generation++; detailGeneration++; if (statusTimer) clearInterval(statusTimer) })
</script>

<template>
  <section class="min-w-0 space-y-5" :aria-busy="loading" data-test="balance-notification-records">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0 flex-1"><h3 class="text-lg font-semibold">{{ tr('title') }}</h3><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ tr('retention') }}</p></div>
      <button type="button" class="btn btn-secondary btn-sm" data-test="notice-refresh" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" />{{ tr('refresh') }}</button>
    </header>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
      <div v-for="item in summary" :key="item.key" class="min-w-0 rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-dark-600 dark:bg-dark-800"><p class="text-xs text-gray-500 dark:text-gray-400">{{ tr(item.key) }}</p><p class="mt-2 text-2xl font-semibold tabular-nums" :class="item.key === 'failed' ? 'text-red-600 dark:text-red-400' : ''">{{ item.count.toLocaleString() }}</p></div>
    </div>
    <form class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5" @submit.prevent="applyFilters">
      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <div><label for="notice-status" class="input-label">{{ tr('status') }}</label><Select id="notice-status" v-model="filters.status" :options="statuses" /></div>
        <div><label for="notice-trigger" class="input-label">{{ tr('trigger') }}</label><Select id="notice-trigger" v-model="filters.trigger_type" :options="triggers" /></div>
        <div><label for="notice-user-id" class="input-label">{{ tr('userId') }}</label><input id="notice-user-id" v-model="filters.user_id" class="input" type="text" inputmode="numeric" autocomplete="off" /></div>
        <div><label for="notice-user-email" class="input-label">{{ tr('userEmail') }}</label><input id="notice-user-email" v-model="filters.user_email" data-test="notice-user-email" class="input" :placeholder="tr('userPlaceholder')" maxlength="320" /></div>
        <div><label for="notice-recipient" class="input-label">{{ tr('recipient') }}</label><input id="notice-recipient" v-model="filters.recipient" data-test="notice-recipient" class="input" :placeholder="tr('recipientPlaceholder')" maxlength="320" /></div>
        <div class="grid grid-cols-1 gap-3 sm:col-span-2 sm:grid-cols-2 xl:col-span-3">
          <label class="min-w-0 text-xs text-gray-500">{{ tr('from') }}<input v-model="filters.from" class="input mt-1" type="datetime-local" step="1" /></label>
          <label class="min-w-0 text-xs text-gray-500">{{ tr('to') }}<input v-model="filters.to" class="input mt-1" type="datetime-local" step="1" /></label>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-3"><button class="btn btn-primary btn-sm" :disabled="loading">{{ tr('filter') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="reset">{{ tr('reset') }}</button><span class="text-xs text-gray-500 dark:text-gray-400">{{ tr('timezone') }}</span></div>
    </form>
    <p v-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/10 dark:text-red-300">{{ error }}</p>
    <p v-if="loading" role="status" class="text-sm text-gray-500">{{ tr('loading') }}</p>
    <div class="min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
      <DataTable :columns="columns" :data="result?.items || []" :loading="loading && !result" :expandable-actions="false">
        <template #empty><div class="space-y-2"><p class="font-medium">{{ tr('empty') }}</p><p class="text-xs text-gray-500">{{ tr('emptyHint') }}</p></div></template>
        <template #cell-user="{ row }"><div class="max-w-[240px] break-all whitespace-normal text-left"><p class="font-medium">{{ row.user_email || `#${row.user_id}` }}</p><p class="mt-1 text-xs text-gray-500">{{ row.user_name }} · #{{ row.user_id }}</p></div></template>
        <template #cell-recipient="{ row }"><span class="block max-w-[240px] break-all whitespace-normal">{{ row.recipient || '—' }}</span></template>
        <template #cell-amount="{ row }"><div class="space-y-1 text-xs tabular-nums"><p>{{ tr('balance') }} <span class="font-medium">{{ money(row.balance) }}</span></p><p class="text-gray-500">{{ tr('threshold') }} {{ money(row.threshold) }}</p></div></template>
        <template #cell-trigger="{ row }"><span class="text-xs">{{ tr(row.trigger_type) }}</span></template>
        <template #cell-result="{ row }"><div class="max-w-[280px] space-y-1.5 text-left"><span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="tone(row)">{{ status(row) }}</span><p class="text-[11px] text-gray-500">{{ tr('attempts', { count: row.attempts }) }}</p><p v-if="reason(row)" class="break-words whitespace-normal text-xs text-gray-600 dark:text-gray-300">{{ reason(row) }}</p></div></template>
        <template #cell-time="{ row }"><div class="space-y-1 text-xs tabular-nums"><time :datetime="row.created_at">{{ formatGovernanceTime(row.created_at) }}</time><p v-if="row.sent_at" class="text-[11px] text-gray-500">{{ tr('sent') }} {{ formatGovernanceTime(row.sent_at) }}</p></div></template>
        <template #cell-actions="{ row }"><button type="button" class="btn btn-secondary btn-sm" data-test="notice-detail" @click="openDetail(row.id)">{{ tr('detail') }}</button></template>
      </DataTable>
    </div>
    <nav v-if="result" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500" :aria-label="tr('title')"><span>{{ tr('page', { page: result.page, pages: result.pages, total: result.total }) }}</span><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || result.page <= 1" data-test="notice-prev" @click="load(result.page - 1)">{{ tr('previous') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || result.page >= result.pages" data-test="notice-next" @click="load(result.page + 1)">{{ tr('next') }}</button></div></nav>
    <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ tr('accepted') }}</p>
    <BaseDialog :show="detailId !== null" :title="tr('detail')" width="wide" @close="closeDetail">
      <p v-if="detailLoading" role="status" class="py-6 text-sm text-gray-500">{{ tr('detailLoading') }}</p>
      <div v-else-if="detailError" role="alert" class="space-y-3 text-sm text-red-600"><p>{{ detailError }}</p><button type="button" class="btn btn-secondary btn-sm" @click="detailId !== null && openDetail(detailId)">{{ tr('refresh') }}</button></div>
      <div v-else-if="detail" class="min-w-0 space-y-5">
        <div class="flex flex-wrap items-center gap-2"><span class="rounded-full px-2 py-1 text-xs" :class="tone(detail)">{{ status(detail) }}</span><span class="text-xs text-gray-500">{{ tr(detail.trigger_type) }} · {{ tr('attempts', { count: detail.attempts }) }}</span></div>
        <dl class="grid gap-4 text-sm sm:grid-cols-2">
          <div v-for="entry in [{ label: 'userEmail', value: detail.user_email }, { label: 'recipient', value: detail.recipient }, { label: 'balance', value: money(detail.balance) }, { label: 'threshold', value: money(detail.threshold) }, { label: 'createdAt', value: formatGovernanceTime(detail.created_at) }, { label: 'lastAttempt', value: formatGovernanceTime(detail.last_attempt_at) }, { label: 'sentAt', value: formatGovernanceTime(detail.sent_at) }, { label: 'updatedAt', value: formatGovernanceTime(detail.updated_at) }]" :key="entry.label" class="min-w-0"><dt class="text-xs text-gray-500">{{ tr(entry.label) }}</dt><dd class="mt-1 break-all">{{ entry.value || '—' }}</dd></div>
        </dl>
        <p v-if="reason(detail)" class="break-words rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900">{{ reason(detail) }}</p>
        <div><h4 class="text-xs font-medium text-gray-500">{{ tr('subject') }}</h4><p class="mt-1 break-words font-medium">{{ detail.subject || '—' }}</p></div>
        <div><h4 class="mb-2 text-sm font-medium">{{ tr('preview') }}</h4><iframe v-if="preview" :title="tr('preview')" :srcdoc="preview" sandbox="" referrerpolicy="no-referrer" class="h-[440px] w-full rounded-lg border border-gray-200 bg-white dark:border-dark-600" /><p v-else class="rounded-lg bg-gray-50 p-6 text-sm text-gray-500 dark:bg-dark-900">{{ tr('noBody') }}</p></div>
        <details v-if="detail.body" class="min-w-0 text-xs"><summary class="cursor-pointer text-gray-500">{{ tr('rawBody') }}</summary><pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-50 p-3 dark:bg-dark-900">{{ detail.body }}</pre></details>
        <div class="space-y-1 break-all text-xs text-gray-500"><p>{{ tr('identifier') }}: {{ detail.notification_id }}</p><p v-if="detail.batch_id">{{ tr('batch') }}: {{ detail.batch_id }}</p></div>
      </div>
    </BaseDialog>
  </section>
</template>
