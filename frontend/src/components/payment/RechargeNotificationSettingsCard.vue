<template>
  <section id="recharge-notification-settings" class="card scroll-mt-40" :aria-label="t('admin.settings.payment.rechargeNotifyTitle')">
    <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div class="min-w-0 flex-1">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.payment.rechargeNotifyTitle') }}</h2>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.rechargeNotifyEnabled') }}</p>
      </div>
      <Toggle
        :aria-label="t('admin.settings.payment.rechargeNotifyEnabled')"
        :model-value="enabled"
        :disabled="loading || saving || loadFailed"
        @update:model-value="setEnabled"
      />
    </header>
    <div class="space-y-4 p-4">
      <p v-if="loading" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
      <div>
        <label for="recharge-notify-emails" class="input-label">{{ t('admin.settings.payment.rechargeNotifyRecipients') }}</label>
        <textarea
          id="recharge-notify-emails"
          v-model="emailsText"
          rows="3"
          class="input mt-1"
          :disabled="loading || saving || loadFailed"
          :placeholder="t('admin.settings.payment.rechargeNotifyRecipientsPlaceholder')"
          autocomplete="off"
          spellcheck="false"
          @input="notice = ''"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.payment.rechargeNotifyRecipientsHint') }}</p>
      </div>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-green-600 dark:text-green-400">{{ notice }}</p>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-primary btn-sm" data-testid="recharge-notify-save" :disabled="loading || saving || loadFailed" @click="save">{{ saving ? t('common.saving') : t('admin.settings.payment.rechargeNotifySave') }}</button>
        <button v-if="loadFailed" type="button" class="btn btn-secondary btn-sm" data-testid="recharge-notify-retry" :disabled="loading" @click="load">{{ t('common.retry') }}</button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '@/api/admin/payment'
import Toggle from '@/components/common/Toggle.vue'

const { t } = useI18n()
const enabled = ref(false)
const emailsText = ref('')
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const error = ref('')
const notice = ref('')
let disposed = false

function setEnabled(value: boolean) {
  if (loading.value || saving.value || loadFailed.value) return
  enabled.value = value
  notice.value = ''
}

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    const { data } = await adminPaymentAPI.getConfig()
    if (disposed) return
    enabled.value = data.admin_recharge_notify_enabled === true
    emailsText.value = Array.isArray(data.admin_recharge_notify_emails) ? data.admin_recharge_notify_emails.join('\n') : ''
    loadFailed.value = false
  } catch {
    if (disposed) return
    loadFailed.value = true
    error.value = t('admin.settings.payment.rechargeNotifyLoadFailed')
  } finally {
    if (!disposed) loading.value = false
  }
}

async function save() {
  if (loading.value || saving.value || loadFailed.value) return
  error.value = ''
  notice.value = ''
  const emails = [...new Set(emailsText.value.split(/[,，\r\n]+/).map(value => value.trim().toLowerCase()).filter(Boolean))]
  if (emails.some(value => value.length > 254 || !/^[^\s<>@]+@[^\s<>@]+\.[^\s<>@]+$/.test(value))) {
    error.value = t('admin.settings.payment.rechargeNotifyRecipientsInvalid')
    return
  }
  if (emails.length > 10) {
    error.value = t('admin.settings.payment.rechargeNotifyRecipientsLimit')
    return
  }
  if (enabled.value && emails.length === 0) {
    error.value = t('admin.settings.payment.rechargeNotifyRecipientsRequired')
    return
  }
  saving.value = true
  try {
    await adminPaymentAPI.updateConfig({ admin_recharge_notify_enabled: enabled.value, admin_recharge_notify_emails: emails })
    if (disposed) return
    emailsText.value = emails.join('\n')
    notice.value = t('common.saved')
  } catch {
    if (!disposed) error.value = t('admin.settings.payment.rechargeNotifySaveFailed')
  } finally {
    if (!disposed) saving.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => { disposed = true })
</script>
