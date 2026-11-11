<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.payment.billRulesTitle') }}</h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billRulesHint') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="addRule">{{ t('admin.settings.payment.billAddRule') }}</button>
    </div>
    <p v-if="rules === null" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.settings.payment.billRulesInvalid') }}</p>
    <div v-for="rule in rules || []" :key="rule.id" class="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
      <div class="grid grid-cols-1 items-end gap-2 sm:grid-cols-[8rem_1fr_5rem_auto]">
        <label class="block text-xs text-gray-600 dark:text-gray-300">
          {{ t('admin.settings.payment.billRuleAmount') }}
          <input :value="rule.amount" type="text" inputmode="decimal" placeholder="10.00" class="input mt-1" @input="updateRule(rule.id, { amount: ($event.target as HTMLInputElement).value })" />
        </label>
        <label class="block min-w-0 text-xs text-gray-600 dark:text-gray-300">
          {{ t('admin.settings.payment.billRuleUrl') }}
          <input :value="rule.qr_url" type="url" placeholder="https://qr.alipay.com/…" class="input mt-1" @input="updateRule(rule.id, { qr_url: ($event.target as HTMLInputElement).value })" />
        </label>
        <label class="block text-xs text-gray-600 dark:text-gray-300">
          {{ t('admin.settings.payment.billRuleSort') }}
          <input :value="rule.sort_order" type="number" min="0" step="1" class="input mt-1" @input="updateRule(rule.id, { sort_order: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <button type="button" class="btn btn-secondary btn-sm mb-1" @click="removeRule(rule.id)">{{ t('common.delete') }}</button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
          <input :checked="rule.enabled" type="checkbox" class="checkbox" @change="updateRule(rule.id, { enabled: ($event.target as HTMLInputElement).checked })" />
          {{ t('common.enabled') }}
        </label>
        <label class="btn btn-secondary btn-sm cursor-pointer" :class="decodingRuleId ? 'pointer-events-none opacity-60' : ''">
          {{ decodingRuleId === rule.id ? t('admin.settings.payment.billQRDecoding') : t('admin.settings.payment.billUploadQR') }}
          <input type="file" accept="image/*" class="sr-only" :aria-label="t('admin.settings.payment.billUploadQR')" :disabled="!!decodingRuleId" @change="uploadQR(rule.id, $event)" />
        </label>
      </div>
    </div>
    <p v-if="validationError && rules?.length" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`admin.settings.payment.${validationError}`) }}</p>
    <p v-if="decodeMessage" role="status" :class="decodeFailed ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'" class="text-xs">{{ decodeMessage }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billQRVerifyHint') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AlipayBillQRRule } from '@/types/payment'
import { decodeAlipayQRImage, isAlipayQRUrl, parseAlipayBillRules, validateAlipayBillRules } from './alipayBillRules'

const props = defineProps<{ modelValue?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const rules = computed(() => parseAlipayBillRules(props.modelValue))
const validationError = computed(() => rules.value && validateAlipayBillRules(rules.value))
const decodingRuleId = ref('')
const decodeMessage = ref('')
const decodeFailed = ref(false)

function updateRules(next: AlipayBillQRRule[]) {
  emit('update:modelValue', JSON.stringify(next))
}

function addRule() {
  if (rules.value === null) return
  updateRules([...rules.value, { id: crypto.randomUUID(), amount: '', qr_url: '', enabled: true, sort_order: rules.value.length }])
}

function updateRule(id: string, patch: Partial<AlipayBillQRRule>) {
  if (rules.value === null) return
  updateRules(rules.value.map(rule => rule.id === id ? { ...rule, ...patch } : rule))
}

function removeRule(id: string) {
  if (rules.value === null) return
  updateRules(rules.value.filter(rule => rule.id !== id))
}

async function uploadQR(id: string, event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  decodingRuleId.value = id
  decodeMessage.value = ''
  decodeFailed.value = false
  try {
    const url = await decodeAlipayQRImage(file)
    if (!url) throw new Error('billQRDecodeFailed')
    if (!isAlipayQRUrl(url)) throw new Error('billRuleUrlInvalid')
    updateRule(id, { qr_url: url.trim() })
    decodeMessage.value = t('admin.settings.payment.billQRDecoded')
  } catch (error) {
    decodeFailed.value = true
    const key = error instanceof Error && ['billQRImageInvalid', 'billRuleUrlInvalid'].includes(error.message) ? error.message : 'billQRDecodeFailed'
    decodeMessage.value = t(`admin.settings.payment.${key}`)
  } finally {
    decodingRuleId.value = ''
    input.value = ''
  }
}
</script>
