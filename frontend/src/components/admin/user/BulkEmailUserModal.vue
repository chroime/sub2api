<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.bulkEmail.title')"
    width="extra-wide"
    :close-on-escape="!sending"
    :show-close-button="!sending"
    @close="handleClose"
  >
    <div class="space-y-5">
      <template v-if="!result">
        <fieldset :disabled="sending || sendUncertain" class="space-y-4">
          <div>
            <label for="bulk-email-template" class="input-label">{{ t('admin.users.bulkEmail.template') }}</label>
            <Select
              id="bulk-email-template"
              v-model="event"
              :options="templateOptions"
              :disabled="sending || sendUncertain"
              data-test="email-template"
            />
            <p class="input-hint">{{ t(`admin.users.bulkEmail.${event === 'system.health_snapshot' ? 'healthHint' : 'balanceHint'}`) }}</p>
          </div>

          <section v-if="event === 'system.health_snapshot'" class="space-y-3">
            <div>
              <h4 class="input-label">{{ t('admin.users.bulkEmail.linksTitle') }}</h4>
              <p class="input-hint">{{ t('admin.users.bulkEmail.linksHint') }}</p>
            </div>
            <div class="grid gap-3 sm:grid-cols-2">
              <div v-for="field in linkFields" :key="field.key" class="min-w-0">
                <label :for="`bulk-email-${field.key}`" class="input-label">{{ t(`admin.users.bulkEmail.${field.label}`) }}</label>
                <input
                  :id="`bulk-email-${field.key}`"
                  v-model="links[field.key]"
                  type="url"
                  class="input"
                  :data-test="`email-link-${field.key}`"
                  @input="onLinkInput"
                />
              </div>
            </div>
            <p v-if="!hasValidLinks" class="text-xs text-amber-700 dark:text-amber-300" data-test="email-links-warning">
              {{ t(`admin.users.bulkEmail.${hasMissingLinks ? 'linksMissing' : 'linksInvalid'}`) }}
            </p>
          </section>

          <div>
            <p class="input-label">{{ t('admin.users.bulkEmail.recipients') }}</p>
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="flex items-center gap-3 rounded-xl border border-gray-200 p-3 text-sm dark:border-dark-600" :class="snapshotIds.length ? 'cursor-pointer' : 'opacity-50'">
                <input v-model="scope" type="radio" value="selected" :disabled="!snapshotIds.length" data-test="scope-selected" />
                <span>{{ t('admin.users.bulkEmail.selected', { count: snapshotIds.length }) }}</span>
              </label>
              <label class="flex cursor-pointer items-center gap-3 rounded-xl border border-gray-200 p-3 text-sm dark:border-dark-600">
                <input v-model="scope" type="radio" value="all" data-test="scope-all" />
                <span>{{ t('admin.users.bulkEmail.all') }}</span>
              </label>
            </div>
            <p v-if="scope === 'all'" class="input-hint">{{ t('admin.users.bulkEmail.allHint') }}</p>
            <p v-else-if="!snapshotIds.length" class="input-hint">{{ t('admin.users.bulkEmail.noSelection') }}</p>
          </div>

          <div class="max-w-xs">
            <label for="bulk-email-locale" class="input-label">{{ t('admin.users.bulkEmail.language') }}</label>
            <Select
              id="bulk-email-locale"
              v-model="mailLocale"
              :options="[{ value: 'zh', label: '中文' }, { value: 'en', label: 'English' }]"
              :disabled="sending || sendUncertain"
              data-test="email-locale"
            />
          </div>
        </fieldset>

        <button type="button" class="btn btn-secondary" :disabled="!canPreview" data-test="preview-email" @click="loadPreview">
          <Icon :name="previewLoading ? 'refresh' : 'eye'" size="sm" class="mr-2" :class="{ 'animate-spin': previewLoading }" />
          {{ previewLoading ? t('common.loading') : t('admin.users.bulkEmail.preview') }}
        </button>

        <section v-if="preview" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.bulkEmail.subject') }}</p>
            <p class="mt-1 font-medium text-gray-900 dark:text-gray-100" data-test="email-preview-subject">{{ preview.subject }}</p>
          </div>
          <iframe
            :srcdoc="preview.html"
            :title="t('admin.users.bulkEmail.preview')"
            sandbox=""
            referrerpolicy="no-referrer"
            class="h-96 w-full rounded-lg border border-gray-200 bg-white dark:border-dark-600"
          />
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.bulkEmail.previewHint') }}</p>
        </section>

        <p v-if="sending" class="rounded-lg bg-primary-50 p-3 text-sm text-primary-700 dark:bg-primary-900/20 dark:text-primary-300" role="status">
          {{ t('admin.users.bulkEmail.sendingHint') }}
        </p>
        <p v-if="error" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300" role="alert">
          {{ error }}<span v-if="sendUncertain" class="mt-1 block">{{ t('admin.users.bulkEmail.unknownResult') }}</span>
        </p>
      </template>

      <section v-else class="space-y-4" data-test="email-results" aria-live="polite">
        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-700/50">
          <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.users.bulkEmail.completed') }}</h4>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
            {{ t('admin.users.bulkEmail.summary', { total: result.total, sent: result.sent, failed: result.failed, skipped: result.skipped }) }}
          </p>
        </div>
        <DataTable :columns="resultColumns" :data="result.items" row-key="user_id">
          <template #cell-status="{ value }">
            <span :class="['badge', value === 'sent' ? 'badge-success' : value === 'failed' ? 'badge-danger' : 'badge-gray']">
              {{ t(`admin.users.bulkEmail.${value}`) }}
            </span>
          </template>
          <template #cell-error="{ value }">
            <span class="break-words text-sm text-gray-600 dark:text-gray-300">{{ recipientErrorMessage(value) }}</span>
          </template>
        </DataTable>
      </section>
    </div>

    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="sending" data-test="close-email" @click="handleClose">
          {{ result ? t('common.close') : t('common.cancel') }}
        </button>
        <button v-if="!result" type="button" class="btn btn-primary" :disabled="!canSend" data-test="send-email" @click="sendEmail">
          {{ sending ? t('admin.users.bulkEmail.sending') : scope === 'all' ? t('admin.users.bulkEmail.confirmAll') : t('admin.users.bulkEmail.confirmSelected', { count: snapshotIds.length }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { BulkUserEmailEvent, BulkUserEmailPreviewResponse, BulkUserEmailResponse, UserBulkEmailLinks } from '@/api/admin/users'
import type { Column } from '@/components/common/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean; selectedIds: number[] }>()
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n()
const snapshotIds = ref<number[]>([])
const scope = ref<'selected' | 'all'>('selected')
const event = ref<BulkUserEmailEvent>('system.health_snapshot')
const mailLocale = ref('zh')
const preview = ref<BulkUserEmailPreviewResponse | null>(null)
const result = ref<BulkUserEmailResponse | null>(null)
const previewLoading = ref(false)
const sending = ref(false)
const sendUncertain = ref(false)
const error = ref('')
const emptyLinks = (): UserBulkEmailLinks => ({ platform_url: '', docs_url: '', model_plaza_url: '', canvas_url: '' })
const links = ref<UserBulkEmailLinks>(emptyLinks())
const linksEdited = ref(false)
const linkFields = [
  { key: 'platform_url', label: 'platformUrl' },
  { key: 'docs_url', label: 'docsUrl' },
  { key: 'model_plaza_url', label: 'modelPlazaUrl' },
  { key: 'canvas_url', label: 'canvasUrl' }
] as const
let previewSequence = 0

const templateOptions = computed(() => [
  { value: 'system.health_snapshot', label: t('admin.users.bulkEmail.healthTemplate') },
  { value: 'balance.low', label: t('admin.users.bulkEmail.balanceTemplate') }
])
const resultColumns = computed<Column[]>(() => [
  { key: 'user_id', label: t('admin.users.columns.id') },
  { key: 'email', label: t('admin.users.email') },
  { key: 'status', label: t('admin.users.bulkEmail.deliveryStatus') },
  { key: 'error', label: t('admin.users.bulkEmail.detail') }
])
const hasRecipients = computed(() => scope.value === 'all' || snapshotIds.value.length > 0)
const normalizedLinks = computed<UserBulkEmailLinks>(() => ({
  platform_url: links.value.platform_url.trim(),
  docs_url: links.value.docs_url.trim(),
  model_plaza_url: links.value.model_plaza_url.trim(),
  canvas_url: links.value.canvas_url.trim()
}))
const hasMissingLinks = computed(() => Object.values(normalizedLinks.value).some(value => !value))
const hasValidLinks = computed(() => Object.values(normalizedLinks.value).every(value => {
  try {
    const url = new URL(value)
    return (url.protocol === 'http:' || url.protocol === 'https:') && !!url.hostname && !url.username && !url.password
  } catch {
    return false
  }
}))
const linkRequest = computed(() => event.value === 'system.health_snapshot' &&
  (linksEdited.value || Object.values(normalizedLinks.value).some(Boolean)) ? { links: normalizedLinks.value } : {})
const canPreview = computed(() => hasRecipients.value && !previewLoading.value && !sending.value && !sendUncertain.value)
const canSend = computed(() => hasRecipients.value && !!preview.value && !previewLoading.value && !sending.value && !sendUncertain.value &&
  (event.value !== 'system.health_snapshot' || hasValidLinks.value))

const recipientErrorCodes = new Set([
  'user_not_found', 'unsubscribed', 'invalid_email', 'balance_above_threshold',
  'balance_threshold_not_configured', 'notification_data_unavailable', 'email_delivery_failed',
  'notification_suppressed', 'user_lookup_failed', 'notification_preference_unavailable', 'request_cancelled',
  'service_links_not_configured'
])

const recipientErrorMessage = (value: unknown): string => {
  if (typeof value !== 'string' || !value.trim()) return '—'
  return recipientErrorCodes.has(value) ? t(`admin.users.bulkEmail.errors.${value}`) : value
}

const invalidatePreview = () => {
  previewSequence++
  preview.value = null
  previewLoading.value = false
  error.value = ''
}

const onLinkInput = () => {
  linksEdited.value = true
  invalidatePreview()
}

watch([event, scope, mailLocale], invalidatePreview)
watch(() => props.show, (show) => {
  invalidatePreview()
  if (show) {
    snapshotIds.value = [...new Set(props.selectedIds)]
    scope.value = 'selected'
    event.value = 'system.health_snapshot'
    mailLocale.value = locale?.value === 'en' ? 'en' : 'zh'
    result.value = null
    sendUncertain.value = false
    links.value = emptyLinks()
    linksEdited.value = false
  }
}, { immediate: true })

const handleClose = () => {
  if (!sending.value) emit('close')
}

const loadPreview = async () => {
  if (!canPreview.value) return
  const sequence = ++previewSequence
  previewLoading.value = true
  preview.value = null
  error.value = ''
  try {
    const response = await adminAPI.users.previewBulkEmail({
      event: event.value,
      locale: mailLocale.value,
      ...(scope.value === 'selected' ? { user_id: snapshotIds.value[0] } : {}),
      ...linkRequest.value
    })
    if (sequence === previewSequence) {
      if (event.value === 'system.health_snapshot' && response.links) {
        links.value = { ...emptyLinks(), ...response.links }
      }
      preview.value = response
    }
  } catch (cause) {
    if (sequence === previewSequence) error.value = extractApiErrorMessage(cause, t('admin.users.bulkEmail.previewFailed'))
  } finally {
    if (sequence === previewSequence) previewLoading.value = false
  }
}

const sendEmail = async () => {
  if (!canSend.value) return
  sending.value = true
  error.value = ''
  try {
    result.value = await adminAPI.users.bulkEmail({
      user_ids: scope.value === 'selected' ? [...snapshotIds.value] : [],
      all: scope.value === 'all',
      event: event.value,
      locale: mailLocale.value,
      ...linkRequest.value
    })
  } catch (cause) {
    error.value = extractApiErrorMessage(cause, t('admin.users.bulkEmail.failed'))
    // A lost response does not prove that mail was not delivered. Never blindly retry the same batch.
    sendUncertain.value = true
  } finally {
    sending.value = false
  }
}

onBeforeUnmount(() => { previewSequence++ })
</script>
