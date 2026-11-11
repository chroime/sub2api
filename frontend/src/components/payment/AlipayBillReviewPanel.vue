<template>
  <BaseDialog :show="show" :title="`${t('admin.settings.payment.billReviewTitle')} · ${provider.name}`" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <Select :model-value="status" :options="statusOptions" class="w-48" :aria-label="t('admin.settings.payment.billReceiptStatus')" @update:model-value="status = String($event); page = 1; loadReceipts()" />
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadReceipts">{{ t('common.refresh') }}</button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="syncing || claiming || !hasReceiverUID" @click="syncBills">{{ syncing ? t('admin.settings.payment.billSyncing') : t('admin.settings.payment.billSync') }}</button>
        </div>
      </div>
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billReviewHint') }}</p>
      <p v-if="!hasReceiverUID" class="text-sm text-amber-600 dark:text-amber-300">{{ t('admin.settings.payment.billSyncReceiverRequired') }}</p>
      <p v-if="syncResult" role="status" class="rounded-lg bg-gray-50 p-3 text-sm text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ t('admin.settings.payment.billSyncResult', syncResult) }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-green-600 dark:text-green-400">{{ notice }}</p>
      <div v-if="loading" class="py-6 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="!receipts.length" class="py-6 text-center text-sm text-gray-500">{{ t('admin.settings.payment.billNoReceipts') }}</div>
      <div v-else class="space-y-2">
        <article v-for="receipt in receipts" :key="receipt.id" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div class="min-w-0 space-y-1 text-xs text-gray-500 dark:text-gray-400">
              <div class="flex flex-wrap items-center gap-2">
                <strong class="text-base text-gray-900 dark:text-white">¥{{ receipt.amount }}</strong>
                <span class="rounded px-2 py-0.5" :class="receipt.status === 'review' ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 dark:bg-dark-700'">{{ receiptStatusLabel(receipt.status) }}</span>
                <span>{{ new Date(receipt.trans_at).toLocaleString() }}</span>
              </div>
              <p class="break-all">{{ t('admin.settings.payment.billAccountLogId') }}: {{ receipt.account_log_id }}</p>
              <p v-if="receipt.alipay_order_no" class="break-all">{{ t('admin.settings.payment.billAlipayOrderNo') }}: {{ receipt.alipay_order_no }}</p>
              <p v-if="receipt.merchant_order_no" class="break-all">{{ t('admin.settings.payment.billMerchantOrderNo') }}: {{ receipt.merchant_order_no }}</p>
              <p v-if="receipt.order_id">{{ t('admin.settings.payment.billClaimedOrder') }} #{{ receipt.order_id }}</p>
              <p v-if="receipt.epay_order_id">{{ t('admin.settings.payment.billClaimedGatewayOrder') }} #{{ receipt.epay_order_id }}</p>
              <p v-if="receipt.status === 'review'" class="text-amber-700 dark:text-amber-300">{{ reviewReasonLabel(receipt.review_reason) }}</p>
            </div>
            <button v-if="receipt.status === 'review'" type="button" class="btn btn-secondary btn-sm" :disabled="claiming" @click="startClaim(receipt)">{{ t('admin.settings.payment.billClaim') }}</button>
          </div>
          <div v-if="claimReceipt?.id === receipt.id" class="mt-3 space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700">
            <label class="block text-xs text-gray-600 dark:text-gray-300">
              {{ t('admin.settings.payment.billClaimTarget') }}
              <Select :model-value="claimTarget" :options="targetOptions" data-testid="bill-claim-target" class="mt-1" :disabled="claiming" @update:model-value="setClaimTarget" />
            </label>
            <label class="block text-xs text-gray-600 dark:text-gray-300">
              {{ t('admin.settings.payment.billSearchOrder') }}
              <div class="mt-1 flex gap-2">
                <input v-model="orderKeyword" type="text" class="input min-w-0 flex-1" :placeholder="t(claimTarget === 'gateway' ? 'admin.settings.payment.billSearchGatewayOrderPlaceholder' : 'admin.settings.payment.billSearchOrderPlaceholder')" @keydown.enter.prevent="searchOrders" />
                <button type="button" class="btn btn-secondary btn-sm shrink-0 whitespace-nowrap" :disabled="searching || !orderKeyword.trim()" @click="searchOrders">{{ t('common.search') }}</button>
              </div>
            </label>
            <p v-if="searching" class="text-xs text-gray-500">{{ t('common.loading') }}</p>
            <div v-else-if="orders.length" class="max-h-52 space-y-1 overflow-y-auto">
              <label v-for="order in orders" :key="order.id" class="flex cursor-pointer items-start gap-2 rounded border p-2 text-xs" :class="selectedOrderId === order.id ? 'border-primary-500' : 'border-gray-200 dark:border-dark-700'">
                <input v-model="selectedOrderId" type="radio" :value="order.id" name="bill-claim-order" class="mt-1" @change="claimVerified = false" />
                <span class="break-all text-gray-700 dark:text-gray-300">#{{ order.id }} · {{ order.out_trade_no }} · {{ order.currency || 'CNY' }} {{ order.amount }} · {{ t(`payment.status.${order.status.toLowerCase()}`) }}<br />
                  <template v-if="claimTarget === 'gateway'">{{ t('admin.settings.payment.billGatewayTradeNo') }}: {{ order.trade_no }}<br />{{ t('admin.settings.payment.billNotifyStatus') }}: {{ notifyStatusLabel(order.notify_status || '') }} · </template>
                  <template v-else>{{ t('admin.settings.payment.billOrderUser') }} #{{ order.user_id }} · </template>{{ new Date(order.created_at).toLocaleString() }}
                </span>
              </label>
            </div>
            <p v-else-if="searched" class="text-xs text-gray-500">{{ t('admin.settings.payment.billNoOrders') }}</p>
            <label class="block text-xs text-gray-600 dark:text-gray-300">
              {{ t('admin.settings.payment.billClaimReason') }}
              <textarea v-model="claimReason" rows="2" maxlength="500" class="input mt-1" :placeholder="t('admin.settings.payment.billClaimReasonPlaceholder')" />
            </label>
            <label class="flex items-start gap-2 text-xs leading-5 text-gray-600 dark:text-gray-300">
              <input v-model="claimVerified" type="checkbox" class="checkbox mt-1" />
              {{ t('admin.settings.payment.billClaimVerified') }}
            </label>
            <div class="flex justify-end gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="claiming" @click="claimReceipt = null">{{ t('common.cancel') }}</button>
              <button type="button" class="btn btn-primary btn-sm" :disabled="!canClaim" @click="claimReceiptToOrder">{{ claiming ? t('common.saving') : t('admin.settings.payment.billConfirmClaim') }}</button>
            </div>
          </div>
        </article>
      </div>
      <div v-if="total > pageSize" class="flex items-center justify-end gap-3 text-xs text-gray-500">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="page--; loadReceipts()">{{ t('admin.settings.payment.billPreviousPage') }}</button>
        <span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="page * pageSize >= total || loading" @click="page++; loadReceipts()">{{ t('admin.settings.payment.billNextPage') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AlipayBillReceipt, AlipayBillSyncResult, ProviderInstance } from '@/types/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean; provider: ProviderInstance }>()
const emit = defineEmits<{ close: [] }>()
const { t, te } = useI18n()
const statusOptions = computed(() => [
  { value: 'review', label: t('admin.settings.payment.billStatusReview') },
  { value: 'matched', label: t('admin.settings.payment.billStatusMatched') },
  { value: '', label: t('common.all') },
])
const targetOptions = computed(() => [
  { value: 'local', label: t('admin.settings.payment.billTargetLocal') },
  { value: 'gateway', label: t('admin.settings.payment.billTargetGateway') },
])
const receipts = ref<AlipayBillReceipt[]>([])
const status = ref('review')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const syncing = ref(false)
const syncResult = ref<AlipayBillSyncResult | null>(null)
const error = ref('')
const notice = ref('')
const claimReceipt = ref<AlipayBillReceipt | null>(null)
const claimTarget = ref<'local' | 'gateway'>('local')
const orderKeyword = ref('')
interface ClaimOrder {
  id: number
  out_trade_no: string
  amount: string | number
  status: string
  created_at: string
  currency?: string
  user_id?: number
  trade_no?: string
  notify_status?: string
}
const orders = ref<ClaimOrder[]>([])
const selectedOrderId = ref<number | null>(null)
const searching = ref(false)
const searched = ref(false)
const claimReason = ref('')
const claimVerified = ref(false)
const claiming = ref(false)
let listRequest = 0
let searchRequest = 0

const canClaim = computed(() => !!claimReceipt.value && !!selectedOrderId.value && claimReason.value.trim().length > 0 && claimReason.value.trim().length <= 500 && claimVerified.value && !claiming.value)
const hasReceiverUID = computed(() => /^2088\d{12}$/.test((props.provider.config.receiverId || '').trim()))

function receiptStatusLabel(value: string) {
  const keys: Record<string, string> = { review: 'billStatusReview', matched: 'billStatusMatched', ignored: 'billStatusIgnored' }
  return t(`admin.settings.payment.${keys[value] || 'billStatusReview'}`)
}

function reviewReasonLabel(reason: string) {
  const key = `admin.settings.payment.billReviewReasons.${reason}`
  return te(key) ? t(key) : t('admin.settings.payment.billReviewReasons.awaiting_review')
}

function notifyStatusLabel(status: string) {
  const key = `admin.settings.payment.billNotifyStatuses.${status.toLowerCase()}`
  return te(key) ? t(key) : status
}

function reportError(value: unknown) {
  error.value = extractI18nErrorMessage(value, t, 'payment.errors', t('common.error'))
}

async function loadReceipts() {
  const request = ++listRequest
  if (!hasReceiverUID.value) {
    receipts.value = []
    total.value = 0
    loading.value = false
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const { data } = await adminPaymentAPI.getAlipayBillReceipts(props.provider.id, { page: page.value, page_size: pageSize, status: status.value })
    if (request !== listRequest) return
    receipts.value = data.items || []
    total.value = data.total
  } catch (value) {
    if (request === listRequest) reportError(value)
  } finally {
    if (request === listRequest) loading.value = false
  }
}

async function syncBills() {
  if (!hasReceiverUID.value || syncing.value) return
  syncing.value = true
  error.value = ''
  notice.value = ''
  syncResult.value = null
  try {
    const { data } = await adminPaymentAPI.syncAlipayBills(props.provider.id)
    syncResult.value = data
    await loadReceipts()
  } catch (value) {
    reportError(value)
  } finally {
    syncing.value = false
  }
}

function resetClaimSearch() {
  searchRequest++
  orderKeyword.value = ''
  orders.value = []
  selectedOrderId.value = null
  searched.value = false
  searching.value = false
  claimReason.value = ''
  claimVerified.value = false
  notice.value = ''
  error.value = ''
}

function startClaim(receipt: AlipayBillReceipt) {
  claimReceipt.value = receipt
  claimTarget.value = 'local'
  resetClaimSearch()
  orderKeyword.value = receipt.merchant_order_no || ''
}

function setClaimTarget(value: unknown) {
  claimTarget.value = value === 'gateway' ? 'gateway' : 'local'
  resetClaimSearch()
}

async function searchOrders() {
  if (!orderKeyword.value.trim()) return
  const request = ++searchRequest
  searching.value = true
  searched.value = false
  selectedOrderId.value = null
  claimVerified.value = false
  orders.value = []
  error.value = ''
  try {
    const params = { page: 1, page_size: 20, keyword: orderKeyword.value.trim() }
    if (claimTarget.value === 'gateway') {
      const { data } = await adminPaymentAPI.getEasyPayOrders(props.provider.id, params)
      if (request !== searchRequest) return
      orders.value = data.items || []
    } else {
      const { data } = await adminPaymentAPI.getOrders({ ...params, payment_type: 'alipay' })
      if (request !== searchRequest) return
      orders.value = (data.items || []).map(order => ({ ...order, amount: order.pay_amount }))
    }
    searched.value = true
  } catch (value) {
    if (request === searchRequest) reportError(value)
  } finally {
    if (request === searchRequest) searching.value = false
  }
}

async function claimReceiptToOrder() {
  if (!canClaim.value || !claimReceipt.value || !selectedOrderId.value) return
  claiming.value = true
  error.value = ''
  try {
    const reason = claimReason.value.trim()
    const target = claimTarget.value === 'gateway'
      ? { epay_order_id: selectedOrderId.value, reason }
      : { order_id: selectedOrderId.value, reason }
    await adminPaymentAPI.claimAlipayBillReceipt(props.provider.id, claimReceipt.value.id, target)
    claimReceipt.value = null
    notice.value = t('admin.settings.payment.billClaimSuccess')
    await loadReceipts()
  } catch (value) {
    reportError(value)
  } finally {
    claiming.value = false
  }
}

watch(() => [props.show, props.provider.id, hasReceiverUID.value], () => {
  searchRequest++
  claimReceipt.value = null
  if (props.show) {
    page.value = 1
    void loadReceipts()
  }
}, { immediate: true })
</script>
