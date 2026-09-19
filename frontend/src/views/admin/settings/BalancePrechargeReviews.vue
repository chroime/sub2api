<template>
  <section class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-600" aria-labelledby="precharge-reviews-title" :aria-busy="loading">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h5 id="precharge-reviews-title" class="scroll-mt-40 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.prechargeReviews.title') }}</h5>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.description') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <select v-model="filter" class="input w-auto text-sm" :aria-label="t('admin.settings.prechargeReviews.filter')" :disabled="loading || saving" data-testid="reviews-filter" @change="load(0)">
          <option value="pending">{{ t('admin.settings.prechargeReviews.pending') }}</option>
          <option value="resolved">{{ t('admin.settings.prechargeReviews.resolved') }}</option>
          <option value="settled">{{ t('admin.settings.prechargeReviews.settled') }}</option>
          <option value="all">{{ t('admin.settings.prechargeReviews.all') }}</option>
        </select>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving" @click="load(offset)">{{ t('admin.settings.prechargeReviews.refresh') }}</button>
      </div>
    </div>
    <p v-if="notice" role="status" class="text-xs text-gray-600 dark:text-gray-300">{{ t(notice) }}</p>
    <div v-if="loadError" role="alert" class="flex flex-wrap items-center gap-3 text-sm text-red-600 dark:text-red-400">
      <span>{{ t('admin.settings.prechargeReviews.loadFailed') }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" data-testid="reviews-retry" @click="load(offset)">{{ t('admin.settings.prechargeReviews.retry') }}</button>
    </div>
    <p v-else-if="loading" role="status" class="py-3 text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
    <p v-else-if="!items.length" data-testid="reviews-empty" class="rounded-lg bg-gray-50 px-4 py-5 text-center text-sm text-gray-500 dark:bg-dark-800 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.empty') }}</p>
    <ul v-else class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-600 dark:border-dark-600">
      <li v-for="item in items" :key="item.id" class="flex flex-col justify-between gap-3 px-4 py-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div class="min-w-0 flex-1">
          <p class="break-all text-sm font-medium text-gray-800 dark:text-gray-200">{{ item.user_email || `#${item.user_id}` }}</p>
          <p class="mt-1 break-words text-xs text-gray-500 dark:text-gray-400">{{ item.group_name || `#${item.group_id}` }} · {{ item.model || '—' }} · {{ formatDateTime(item.created_at) }}</p>
        </div>
        <div class="flex items-center justify-between gap-3">
          <div class="sm:text-right">
            <p class="font-mono text-sm tabular-nums text-gray-800 dark:text-gray-200">${{ money(item.amount) }}</p>
            <span class="text-xs" :class="item.status === 'pending' ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500 dark:text-gray-400'">{{ t(`admin.settings.prechargeReviews.${item.status}`) }}</span>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" data-testid="review-open" :disabled="saving" @click="open(item)">{{ t(item.status === 'pending' ? 'admin.settings.prechargeReviews.handle' : 'admin.settings.prechargeReviews.details') }}</button>
        </div>
      </li>
    </ul>
    <div v-if="total > 0 && !loadError" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
      <span>{{ t('admin.settings.prechargeReviews.page', { page: Math.floor(offset / pageSize) + 1, pages: Math.max(1, Math.ceil(total / pageSize)), total }) }}</span>
      <div class="flex gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving || offset === 0" data-testid="reviews-previous" @click="load(Math.max(0, offset - pageSize))">{{ t('pagination.previous') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving || offset + pageSize >= total" data-testid="reviews-next" @click="load(offset + pageSize)">{{ t('pagination.next') }}</button>
      </div>
    </div>

    <BaseDialog :show="!!selected" :title="t(isPending ? 'admin.settings.prechargeReviews.handle' : 'admin.settings.prechargeReviews.details')" :close-on-escape="!saving" :show-close-button="!saving" @close="close">
      <div v-if="selected" class="space-y-5">
        <dl class="grid grid-cols-1 gap-x-4 gap-y-2 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.user') }}</dt>
          <dd class="break-all text-gray-900 dark:text-gray-100">{{ selected.user_email || '—' }} <span class="text-xs text-gray-500">#{{ selected.user_id }}</span></dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.group') }}</dt>
          <dd class="break-all text-gray-800 dark:text-gray-200">{{ selected.group_name || '—' }} <span class="text-xs text-gray-500">#{{ selected.group_id }}</span></dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.model') }}</dt>
          <dd class="break-all text-gray-800 dark:text-gray-200">{{ selected.model || '—' }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.account') }}</dt>
          <dd class="break-all text-gray-800 dark:text-gray-200">{{ selected.account_id || '—' }} / {{ selected.api_key_id }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.holdId') }}</dt>
          <dd class="break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ selected.id }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.requestId') }}</dt>
          <dd class="break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ selected.request_id || '—' }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.reason') }}</dt>
          <dd class="break-words text-gray-800 dark:text-gray-200">{{ reviewReason }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.frozenAmount') }}</dt>
          <dd class="font-mono font-semibold tabular-nums text-gray-900 dark:text-gray-100">${{ money(selected.amount) }}</dd>
        </dl>

        <template v-if="isPending">
          <fieldset :disabled="saving" class="space-y-3">
            <legend class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.prechargeReviews.action') }}</legend>
            <div class="flex flex-wrap gap-x-5 gap-y-2 text-sm text-gray-700 dark:text-gray-300">
              <label class="flex cursor-pointer items-center gap-2"><input v-model="action" type="radio" name="precharge-resolution" value="release" data-testid="review-action-release" />{{ t('admin.settings.prechargeReviews.release') }}</label>
              <label class="flex cursor-pointer items-center gap-2"><input v-model="action" type="radio" name="precharge-resolution" value="charge" data-testid="review-action-charge" />{{ t('admin.settings.prechargeReviews.charge') }}</label>
            </div>
            <div v-if="action === 'charge'">
              <label for="precharge-review-cost" class="input-label">{{ t('admin.settings.prechargeReviews.actualCost') }}</label>
              <input id="precharge-review-cost" v-model="costInput" type="text" inputmode="decimal" class="input" :aria-invalid="costInput !== '' && !validCost" aria-describedby="precharge-review-cost-hint" data-testid="review-cost" />
              <p id="precharge-review-cost-hint" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.costHint') }}</p>
            </div>
            <div>
              <label for="precharge-review-note" class="input-label">{{ t('admin.settings.prechargeReviews.note') }}</label>
              <textarea id="precharge-review-note" v-model="note" rows="3" class="input" :aria-invalid="noteLength > MAX_PRECHARGE_REVIEW_NOTE_LENGTH" aria-describedby="precharge-review-note-hint" data-testid="review-note" />
              <p id="precharge-review-note-hint" class="mt-1 text-xs" :class="noteLength > MAX_PRECHARGE_REVIEW_NOTE_LENGTH ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">{{ t('admin.settings.prechargeReviews.noteHint') }} · {{ noteLength }}/{{ MAX_PRECHARGE_REVIEW_NOTE_LENGTH }}</p>
            </div>
          </fieldset>
          <div v-if="validCost" class="space-y-1 rounded-lg border border-primary-200 bg-primary-50 p-3 text-sm text-primary-900 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-200">
            <p>{{ t('admin.settings.prechargeReviews.costSummary', { amount: money(actualCost!) }) }}</p>
            <p data-testid="review-delta" class="font-medium">{{ deltaText }}</p>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.prechargeReviews.accountingOnly') }}</p>
          <p v-if="resolveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.settings.prechargeReviews.resolveFailed') }}</p>
        </template>
        <div v-else class="space-y-2 text-sm text-gray-700 dark:text-gray-300">
          <p v-if="selected.status === 'settled'">{{ t('admin.settings.prechargeReviews.settledDescription') }}</p>
          <p v-else>{{ t(selected.resolution === 'release' ? 'admin.settings.prechargeReviews.release' : 'admin.settings.prechargeReviews.charge') }}</p>
          <p v-if="selected.actual_cost != null">{{ t('admin.settings.prechargeReviews.costSummary', { amount: money(selected.actual_cost) }) }}</p>
          <p v-if="selected.resolved_by != null">{{ t('admin.settings.prechargeReviews.operator', { id: selected.resolved_by }) }}</p>
          <p v-if="selected.resolved_at">{{ t('admin.settings.prechargeReviews.resolvedTime', { time: formatDateTime(selected.resolved_at) }) }}</p>
          <p v-if="selected.note" class="whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 dark:bg-dark-800">{{ selected.note }}</p>
        </div>
      </div>
      <template #footer>
        <div class="flex flex-wrap justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="saving" data-testid="review-cancel" @click="close">{{ t(isPending ? 'common.cancel' : 'common.close') }}</button>
          <button v-if="isPending" type="button" class="btn btn-primary" :disabled="saving || !valid" data-testid="review-confirm" @click="resolve">
            {{ t(saving ? 'admin.settings.saving' : action === 'release' ? 'admin.settings.prechargeReviews.confirmRelease' : 'admin.settings.prechargeReviews.confirmCharge') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getBalancePrechargeReviews, resolveBalancePrechargeReview, MAX_PRECHARGE_REVIEW_NOTE_LENGTH } from '@/api/admin/balancePrechargeReviews'
import type { BalancePrechargeReview, BalancePrechargeReviewFilter } from '@/api/admin/balancePrechargeReviews'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatBalancePrechargeMoney as money, parseBalancePrechargeMoney } from '@/utils/balancePrecharge'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const pageSize = 20
const filter = ref<BalancePrechargeReviewFilter>('pending')
const items = ref<BalancePrechargeReview[]>([])
const total = ref(0)
const offset = ref(0)
const loading = ref(false)
const loadError = ref(false)
const notice = ref('')
const selected = ref<BalancePrechargeReview | null>(null)
const action = ref<'release' | 'charge'>('release')
const costInput = ref('')
const note = ref('')
const saving = ref(false)
const resolveError = ref(false)
const isPending = computed(() => selected.value?.status === 'pending')
const actualCost = computed(() => action.value === 'release' ? 0 : parseBalancePrechargeMoney(costInput.value))
const validCost = computed(() => actualCost.value !== null && (action.value === 'release' || actualCost.value > 0))
const noteLength = computed(() => [...note.value.trim()].length)
const valid = computed(() => isPending.value && validCost.value && noteLength.value > 0 && noteLength.value <= MAX_PRECHARGE_REVIEW_NOTE_LENGTH)
const reviewReason = computed(() => {
  const reasonKeys: Record<string, string> = {
    usage_missing: 'reasonUsageMissing',
    responses_stream_usage_missing: 'reasonUsageMissing',
    billing_failed: 'reasonBillingFailed',
    release_failed: 'reasonReleaseFailed',
    owner_lease_expired: 'reasonOwnerExpired',
    legacy_owner_unknown: 'reasonLegacyOwner',
    upstream_outcome_unknown: 'reasonOutcomeUnknown',
    upstream_unavailable_usage_missing: 'reasonUnavailableUsageMissing',
    upstream_rate_limited_usage_missing: 'reasonRateLimitedUsageMissing',
    responses_stream_unsettled_retry: 'reasonUnsettledRetry'
  }
  return t(`admin.settings.prechargeReviews.${reasonKeys[selected.value?.reason || ''] || 'reasonOutcomeUnknown'}`)
})
const deltaText = computed(() => {
  if (!selected.value || actualCost.value === null) return ''
  const delta = selected.value.amount - actualCost.value
  return t(delta >= 0 ? 'admin.settings.prechargeReviews.refundDelta' : 'admin.settings.prechargeReviews.additionalChargeDelta', { amount: money(Math.abs(delta)) })
})

async function load(nextOffset = offset.value) {
  if (loading.value) return
  loading.value = true
  loadError.value = false
  offset.value = nextOffset
  try {
    let result = await getBalancePrechargeReviews({ status: filter.value, limit: pageSize, offset: nextOffset })
    if (nextOffset > 0 && !result.items.length) {
      offset.value = Math.max(0, Math.ceil(result.total / pageSize) - 1) * pageSize
      result = await getBalancePrechargeReviews({ status: filter.value, limit: pageSize, offset: offset.value })
    }
    items.value = result.items
    total.value = result.total
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function open(item: BalancePrechargeReview) {
  if (saving.value) return
  selected.value = item
  action.value = 'release'
  costInput.value = ''
  note.value = ''
  resolveError.value = false
  notice.value = ''
}

function close() {
  if (!saving.value) selected.value = null
}

async function resolve() {
  if (saving.value || !valid.value || !selected.value || actualCost.value === null) return
  saving.value = true
  resolveError.value = false
  try {
    await resolveBalancePrechargeReview(selected.value.id, { action: action.value, actual_cost: actualCost.value, note: note.value.trim() })
    selected.value = null
    notice.value = 'admin.settings.prechargeReviews.saved'
    await load()
  } catch (error) {
    if ((error as { status?: number } | null)?.status === 409) {
      selected.value = null
      notice.value = 'admin.settings.prechargeReviews.alreadyHandled'
      await load()
    } else {
      resolveError.value = true
    }
  } finally {
    saving.value = false
  }
}

onMounted(() => { void load(0) })
</script>
