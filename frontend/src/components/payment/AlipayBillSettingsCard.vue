<template>
  <section id="builtin-epay-settings" class="card scroll-mt-40" :aria-label="t('admin.settings.payment.billGatewayTitle')">
    <div class="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div class="min-w-0 flex-1">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.payment.billGatewayTitle') }}</h2>
        <p class="mt-1 max-w-3xl text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billGatewayHint') }}</p>
      </div>
      <Toggle
        v-if="chosenId"
        :model-value="enabled"
        :disabled="loading || saving || !chosenId"
        :aria-label="t('admin.settings.payment.billEnableCollection')"
        @update:model-value="setCollectionEnabled"
      />
    </div>
    <div class="space-y-4 p-4">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <label v-if="billProviders.length" class="block w-full max-w-md text-sm text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.payment.billSavedInstance') }}
          <Select data-testid="bill-instance" :model-value="chosenId" :options="instanceOptions" class="mt-1" :disabled="saving || loading" @update:model-value="chooseInstance(String($event))" />
        </label>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving" @click="newDraft">{{ t('admin.settings.payment.billNewDraft') }}</button>
      </div>
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="!chosenId" class="text-sm text-gray-500">{{ t('admin.settings.payment.billSelectInstanceHint') }}</p>
      <fieldset v-else :disabled="saving || loading" class="space-y-4">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <label class="block w-full max-w-md text-sm text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.payment.providerName') }}
            <input v-model="name" data-testid="bill-name" class="input mt-1" type="text" />
          </label>
        </div>
        <ProviderConfigFields :model-value="config" :fields="credentialFields" :editing="!!selectedSaved" field-prefix="bill-config" @update:model-value="updateCredentialConfig" />
        <p v-if="credentialsLoading" role="status" class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billCredentialsLoading') }}</p>
        <div v-if="credentialsError" class="flex flex-wrap items-center gap-2">
          <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ credentialsError }}</p>
          <button data-testid="bill-credentials-retry" type="button" class="btn btn-secondary btn-sm" :disabled="!selectedSaved || credentialsLoading" @click="selectedSaved && loadEditCredentials(selectedSaved.id)">{{ t('common.retry') }}</button>
        </div>
        <AlipayBillRulesEditor v-model="config.qrRules" />
        <p class="rounded-lg bg-gray-50 p-3 text-xs leading-5 text-gray-600 dark:bg-dark-800 dark:text-gray-300">{{ t('admin.settings.payment.billAmountLockHint') }}</p>
        <details class="rounded-lg border border-gray-200 p-3 dark:border-dark-700" :open="epayEnabled">
          <summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.payment.billCompatibilityTitle') }}</summary>
          <div class="mt-3 space-y-3">
            <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billCompatibilityHint') }}</p>
            <label class="flex items-center gap-2 text-sm"><input v-model="epayEnabled" data-testid="bill-epay-enabled" type="checkbox" class="checkbox" />{{ t('admin.settings.payment.billEnableAPI') }}</label>
            <div class="grid gap-3 sm:grid-cols-[1fr_8rem]">
              <label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('admin.settings.payment.billAPIBase') }}<input :value="apiBase" class="input mt-1" readonly /></label>
              <label class="block text-xs text-gray-600 dark:text-gray-300">PID<input :value="selectedSaved?.id ?? t('admin.settings.payment.billSaveFirst')" class="input mt-1" readonly /></label>
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="copyText(apiBase)">{{ t('admin.settings.payment.billCopyAPIBase') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="!selectedSaved" @click="selectedSaved && copyText(String(selectedSaved.id))">{{ t('admin.settings.payment.billCopyPID') }}</button>
            </div>
            <label for="bill-epay-key" class="input-label">{{ t('admin.settings.payment.billAPIKey') }}<span v-if="keyConfigured" class="ml-2 text-xs text-green-600">{{ t('admin.settings.payment.billKeyConfigured') }}</span></label>
            <input id="bill-epay-key" v-model="config.epayKey" :type="keyVisible ? 'text' : 'password'" class="input font-mono text-xs" autocomplete="new-password" data-1p-ignore data-lpignore="true" data-bwignore="true" :placeholder="keyConfigured ? t('admin.accounts.leaveEmptyToKeep') : t('admin.settings.payment.billKeyPlaceholder')" />
            <div class="flex flex-wrap gap-2">
              <button data-testid="bill-generate-key" type="button" class="btn btn-secondary btn-sm" @click="generateKey">{{ t('admin.settings.payment.billGenerateKey') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="!config.epayKey" @click="keyVisible = !keyVisible">{{ t('common.view') }}</button>
              <button data-testid="bill-copy-key" type="button" class="btn btn-secondary btn-sm" :disabled="!config.epayKey" @click="copyText(config.epayKey)">{{ t('admin.settings.payment.billCopyKey') }}</button>
            </div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billKeyHint') }}</p>
            <label for="bill-notify-origins" class="input-label">{{ t('admin.settings.payment.billNotifyOrigins') }}</label>
            <textarea id="bill-notify-origins" v-model="originsText" rows="3" class="input font-mono text-xs" :placeholder="origin" />
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billOriginsHint') }}</p>
          </div>
        </details>
        <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <p v-if="notice" role="status" class="text-sm text-green-600 dark:text-green-400">{{ notice }}</p>
        <div class="flex flex-wrap gap-2">
          <button data-testid="bill-save" type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="save">{{ saving ? t('common.saving') : enabled ? t('common.save') : t('admin.settings.payment.billSaveDraft') }}</button>
          <button data-testid="bill-test" type="button" class="btn btn-secondary btn-sm" :disabled="!selectedSaved || testing || dirty" @click="testConnection">{{ testing ? t('admin.settings.payment.billTesting') : t('admin.settings.payment.billTestConnection') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="!selectedSaved" @click="reviewOpen = true">{{ t('admin.settings.payment.billReviewTitle') }}</button>
          <button v-if="selectedSaved" data-testid="bill-delete" type="button" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="emit('delete', selectedSaved)">{{ t('common.delete') }}</button>
        </div>
        <p v-if="!selectedSaved || dirty" class="text-xs text-gray-500">{{ t('admin.settings.payment.billTestSavedHint') }}</p>
        <div v-if="testResult" class="space-y-1 text-xs">
          <p class="text-green-600 dark:text-green-400">{{ t('admin.settings.payment.billTestSuccess', { count: testResult.entry_count, time: new Date(testResult.checked_at).toLocaleString() }) }}</p>
          <p class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.billTestReceiptHint') }}</p>
        </div>
      </fieldset>
    </div>
    <AlipayBillReviewPanel v-if="reviewOpen && selectedSaved" :show="reviewOpen" :provider="selectedSaved" @close="reviewOpen = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { AlipayBillTestResult, ProviderInstance } from '@/types/payment'
import ProviderConfigFields from './ProviderConfigFields.vue'
import AlipayBillRulesEditor from './AlipayBillRulesEditor.vue'
import AlipayBillReviewPanel from './AlipayBillReviewPanel.vue'
import { PROVIDER_CONFIG_FIELDS } from './providerConfig'
import { parseAlipayBillRules, serializeAlipayBillRules, validateAlipayBillRules } from './alipayBillRules'
import { generateEasyPayKey, parseEasyPayNotifyOrigins } from './alipayBillGateway'

const props = defineProps<{ providers: ProviderInstance[]; loading: boolean }>()
const emit = defineEmits<{ saved: [provider: ProviderInstance]; delete: [provider: ProviderInstance] }>()
const { t } = useI18n()
const origin = window.location.origin
const apiBase = `${origin}/api/v1/payment/epay`
const billProviders = computed(() => props.providers.filter(provider => provider.provider_key === 'alipay_bill'))
const chosenId = ref('')
const instanceOptions = computed(() => [
  { value: '', label: t('admin.settings.payment.billSelectInstanceHint') },
  ...(chosenId.value === 'new' ? [{ value: 'new', label: t('admin.settings.payment.billNewDraft') }] : []),
  ...billProviders.value.map(provider => ({ value: String(provider.id), label: `${provider.name} · PID ${provider.id}` })),
])
const selectedSaved = ref<ProviderInstance | null>(null)
const name = ref('')
const enabled = ref(false)
function setCollectionEnabled(value: boolean) {
  if (props.loading || saving.value || !chosenId.value) return
  enabled.value = value
}
const config = ref<Record<string, string>>({})
const epayEnabled = ref(false)
const originsText = ref(origin)
const keyConfigured = ref(false)
const keyVisible = ref(false)
const saving = ref(false)
const testing = ref(false)
const error = ref('')
const notice = ref('')
const testResult = ref<AlipayBillTestResult | null>(null)
const reviewOpen = ref(false)
const savedFingerprint = ref('')
const savedConfig = ref<Record<string, string>>({})
const credentialsLoading = ref(false)
const credentialsError = ref('')
const editCredentialKeys = ['privateKey', 'publicKey'] as const
const editedCredentialKeys = new Set<string>()
let credentialsRequest = 0
let disposed = false
let initialized = false
const credentialFields = computed(() => PROVIDER_CONFIG_FIELDS.alipay_bill.map(field => ({
  ...field,
  label: field.key === 'privateKey' ? t('admin.settings.payment.billApplicationPrivateKey')
    : field.key === 'publicKey' ? t('admin.settings.payment.billAlipayPublicKey') : field.label,
  optional: field.key === 'receiverId' ? !enabled.value : field.optional,
})))
const fingerprint = computed(() => JSON.stringify([name.value, enabled.value, epayEnabled.value, originsText.value]))
const dirty = computed(() => fingerprint.value !== savedFingerprint.value || configFingerprint(config.value) !== configFingerprint(savedConfig.value))

function configFingerprint(value: Record<string, string>) {
  return JSON.stringify(Object.entries(value).filter(([, field]) => field !== '').sort(([left], [right]) => left.localeCompare(right)))
}

function invalidateCredentialsRequest() {
  credentialsRequest++
  credentialsLoading.value = false
}

function updateCredentialConfig(value: Record<string, string>) {
  for (const key of editCredentialKeys) {
    if (value[key] !== config.value[key]) editedCredentialKeys.add(key)
  }
  config.value = value
}

async function loadEditCredentials(providerId: number) {
  if (disposed || selectedSaved.value?.id !== providerId) return
  const request = ++credentialsRequest
  credentialsLoading.value = true
  credentialsError.value = ''
  try {
    const { data } = await adminPaymentAPI.getAlipayBillEditCredentials(providerId)
    if (disposed || request !== credentialsRequest || selectedSaved.value?.id !== providerId) return
    if (!data || editCredentialKeys.some(key => typeof data[key] !== 'string')) throw new Error('Invalid credential response')
    const nextConfig = { ...config.value }
    const nextSavedConfig = { ...savedConfig.value }
    for (const key of editCredentialKeys) {
      nextSavedConfig[key] = data[key]
      // Input may have changed while this request was pending or before a retry.
      if (!editedCredentialKeys.has(key)) nextConfig[key] = data[key]
    }
    savedConfig.value = nextSavedConfig
    config.value = nextConfig
  } catch {
    if (!disposed && request === credentialsRequest && selectedSaved.value?.id === providerId) {
      credentialsError.value = t('admin.settings.payment.billCredentialsLoadFailed')
    }
  } finally {
    if (!disposed && request === credentialsRequest) credentialsLoading.value = false
  }
}

function load(provider: ProviderInstance | null) {
  invalidateCredentialsRequest()
  editedCredentialKeys.clear()
  credentialsError.value = ''
  selectedSaved.value = provider
  chosenId.value = provider ? String(provider.id) : 'new'
  name.value = provider?.name || t('admin.settings.payment.billGatewayTitle')
  enabled.value = provider?.enabled ?? false
  config.value = {}
  for (const field of PROVIDER_CONFIG_FIELDS.alipay_bill) {
    if (!field.sensitive && provider?.config[field.key]) config.value[field.key] = provider.config[field.key]
  }
  config.value.qrRules = provider?.config.qrRules || '[]'
  epayEnabled.value = provider?.config.epayEnabled === 'true'
  keyConfigured.value = provider?.config.epayKeyConfigured === 'true'
  originsText.value = origin
  if (provider?.config.epayNotifyOrigins) {
    try {
      const values: unknown = JSON.parse(provider.config.epayNotifyOrigins)
      originsText.value = Array.isArray(values) && values.every(value => typeof value === 'string') ? values.join('\n') : provider.config.epayNotifyOrigins
    } catch { originsText.value = provider.config.epayNotifyOrigins }
  }
  keyVisible.value = false
  error.value = ''
  notice.value = ''
  testResult.value = null
  reviewOpen.value = false
  savedFingerprint.value = fingerprint.value
  savedConfig.value = { ...config.value }
  if (provider) void loadEditCredentials(provider.id)
}

function newDraft() { initialized = true; load(null) }
function chooseInstance(id: string) {
  initialized = true
  if (id === 'new') { load(null); return }
  const provider = billProviders.value.find(item => String(item.id) === id)
  if (provider) load(provider)
  else { load(null); chosenId.value = '' }
}
function generateKey() { config.value.epayKey = generateEasyPayKey(); keyVisible.value = true }
async function copyText(text: string) {
  try { await navigator.clipboard.writeText(text); notice.value = t('common.copied') }
  catch { error.value = t('common.copyFailed') }
}
function fail(key: string) { error.value = t(`admin.settings.payment.${key}`); return false }

async function save() {
  if (saving.value || !chosenId.value) return
  error.value = ''; notice.value = ''
  if (!name.value.trim()) return fail('validationNameRequired')
  const receiver = (config.value.receiverId || '').trim()
  if (enabled.value && !receiver) return fail('billReceiverRequired')
  if (receiver && !/^2088\d{12}$/.test(receiver)) return fail('billReceiverInvalid')
  const rules = parseAlipayBillRules(config.value.qrRules)
  const rulesError = rules === null ? 'billRulesInvalid' : !enabled.value && !rules.length ? null : validateAlipayBillRules(rules)
  if (rulesError) return fail(rulesError)
  for (const field of credentialFields.value) {
    if (field.optional || (selectedSaved.value && field.sensitive)) continue
    if (!(config.value[field.key] || '').trim()) {
      error.value = t('admin.settings.payment.validationFieldRequired', { field: field.label || t(`admin.settings.payment.field_${field.key}`) })
      return
    }
  }
  const key = config.value.epayKey || ''
  if ((key && key.length < 32) || (epayEnabled.value && !key && !keyConfigured.value)) return fail('billKeyInvalid')
  const origins = parseEasyPayNotifyOrigins(originsText.value)
  if (origins === null || (epayEnabled.value && !origins.length)) return fail('billOriginsInvalid')
  const nextConfig: Record<string, string> = {
    qrRules: rules!.length ? serializeAlipayBillRules(rules!) : '[]',
    epayEnabled: String(epayEnabled.value), epayNotifyOrigins: JSON.stringify(origins),
  }
  for (const field of PROVIDER_CONFIG_FIELDS.alipay_bill) {
    const value = (config.value[field.key] || '').trim()
    if (selectedSaved.value && editCredentialKeys.some(key => key === field.key) && value === (savedConfig.value[field.key] || '').trim()) continue
    if (value) nextConfig[field.key] = value
  }
  if (key) nextConfig.epayKey = key
  const payload: Partial<ProviderInstance> = { provider_key: 'alipay_bill', name: name.value.trim(), enabled: enabled.value, supported_types: ['alipay'], payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, config: nextConfig }
  saving.value = true
  const resumeCredentialRead = credentialsLoading.value
  invalidateCredentialsRequest()
  try {
    const response = selectedSaved.value ? await adminPaymentAPI.updateProvider(selectedSaved.value.id, payload) : await adminPaymentAPI.createProvider(payload)
    if (disposed) return
    load(response.data)
    notice.value = t('common.saved')
    emit('saved', response.data)
  } catch (value) {
    if (disposed) return
    error.value = extractI18nErrorMessage(value, t, 'payment.errors', t('common.error'))
    if (resumeCredentialRead && selectedSaved.value) void loadEditCredentials(selectedSaved.value.id)
  } finally { saving.value = false }
}

async function testConnection() {
  if (!selectedSaved.value || dirty.value || testing.value) return
  testing.value = true; error.value = ''; testResult.value = null
  try { testResult.value = (await adminPaymentAPI.testAlipayBillConnection(selectedSaved.value.id)).data }
  catch (value) { error.value = extractI18nErrorMessage(value, t, 'payment.errors', t('common.error')) }
  finally { testing.value = false }
}

watch(() => [props.providers, props.loading] as const, () => {
  if (props.loading) return
  if (!initialized) {
    initialized = true
    if (billProviders.value.length === 1) load(billProviders.value[0])
    else if (!billProviders.value.length) load(null)
  } else if (selectedSaved.value) {
    const current = billProviders.value.find(provider => provider.id === selectedSaved.value?.id)
    if (current && !dirty.value && !editedCredentialKeys.size) load(current)
    else if (!current) {
      load(null)
      if (billProviders.value.length === 1) load(billProviders.value[0])
      else if (billProviders.value.length > 1) { chosenId.value = ''; selectedSaved.value = null }
    }
  }
}, { immediate: true })

onBeforeUnmount(() => {
  disposed = true
  credentialsRequest++
})
</script>
