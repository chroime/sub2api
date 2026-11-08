<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { clearConnectionSecrets, type ConnectionForm } from './connection-form'
import { parseSessionImport, SessionImportError, type SessionImportErrorCode } from './session-import'

const form = defineModel<ConnectionForm>({ required: true })
const props = defineProps<{ idPrefix: string; challenge?: string; disabled?: boolean; siteUrl?: string; platform?: string }>()
const { t } = useI18n()
const advanced = ref(false)
const importJson = ref('')
const importError = ref<SessionImportErrorCode | 'unknown' | ''>('')
const importReady = ref(false)
const modeOptions = computed(() => [
  { value: 'password', label: t('governance.passwordLogin') },
  { value: 'session', label: t('governance.sessionLogin') },
])
const siteLoginUrl = computed(() => {
  try {
    const url = new URL(props.siteUrl || '')
    if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) return ''
    url.search = ''; url.hash = ''
    return url.toString()
  } catch { return '' }
})
function targetIdentity(raw?: string) {
  try {
    const url = new URL(raw || '')
    return url.origin + url.pathname.replace(/\/+$/, '')
  } catch { return (raw || '').trim() }
}

function resetImportState() {
  importJson.value = ''; importError.value = ''; importReady.value = false
}
function clearSessionFields() {
  Object.assign(form.value, { sessionToken: '', refreshToken: '', userAgent: '', expiresIn: undefined, sessionExpiresAt: undefined, userId: undefined })
}
watch(() => form.value.mode, () => {
  clearConnectionSecrets(form.value)
  resetImportState()
  advanced.value = false
}, { flush: 'sync' })
watch(() => form.value, () => { resetImportState(); advanced.value = false }, { flush: 'sync' })
watch(() => form.value.sessionToken, (value, previous) => {
  if (!value && previous) resetImportState()
}, { flush: 'sync' })
watch(() => [props.siteUrl, props.platform], ([url, platform], [previousUrl, previousPlatform]) => {
  // Initial detection may add a platform or normalize a trailing slash.
  if (targetIdentity(url) !== targetIdentity(previousUrl) || previousPlatform && platform !== previousPlatform) {
    clearConnectionSecrets(form.value)
  }
  resetImportState()
}, { flush: 'sync' })

function switchToSession() {
  if (!props.disabled) form.value.mode = 'session'
}
function editImportJson(event: Event) {
  if (props.disabled) return
  clearSessionFields()
  importJson.value = (event.target as HTMLTextAreaElement).value
  importError.value = ''; importReady.value = false
}
function applySessionImport() {
  if (props.disabled || form.value.mode !== 'session' || importReady.value) return
  importError.value = ''; importReady.value = false
  try {
    const parsed = parseSessionImport(importJson.value)
    Object.assign(form.value, {
      sessionToken: parsed.sessionToken, refreshToken: parsed.refreshToken, userAgent: parsed.userAgent,
      expiresIn: parsed.expiresIn, sessionExpiresAt: parsed.sessionExpiresAt, userId: undefined,
    })
    importReady.value = true
  } catch (error) {
    importError.value = error instanceof SessionImportError ? error.code : 'unknown'
  }
}
function clearSessionImport() {
  if (props.disabled) return
  resetImportState()
  clearSessionFields()
}
function editSessionText(field: 'sessionToken' | 'refreshToken' | 'userAgent', event: Event) {
  if (props.disabled) return
  form.value[field] = (event.target as HTMLInputElement).value
  if (field === 'sessionToken') form.value.sessionExpiresAt = undefined
  resetImportState()
}
function editSessionNumber(field: 'expiresIn' | 'userId', event: Event) {
  if (props.disabled) return
  const input = event.target as HTMLInputElement
  form.value[field] = input.value === '' || Number.isNaN(input.valueAsNumber) ? undefined : input.valueAsNumber
  if (field === 'expiresIn') form.value.sessionExpiresAt = undefined
  resetImportState()
}
</script>

<template>
  <fieldset :disabled="disabled" class="space-y-4">
    <div>
      <label class="mb-1 block" :for="idPrefix + '-auth-mode'">{{ t('governance.authMode') }}</label>
      <Select :id="idPrefix + '-auth-mode'" v-model="form.mode" :options="modeOptions" :searchable="false" :disabled="disabled" :aria-label="t('governance.authMode')" />
    </div>
    <div v-if="challenge && form.mode === 'password'" role="status" class="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
      <p>{{ t(challenge === 'totp' ? 'governance.totp' : challenge === 'captcha' ? 'governance.sessionImport.description' : 'governance.unsupportedChallenge') }}</p>
      <span v-if="challenge === 'captcha' && form.captchaProvider" class="block font-medium">{{ t(`governance.captchaProvider_${form.captchaProvider}`) }}</span>
      <button v-if="challenge === 'captcha'" :id="idPrefix + '-session-import-switch'" type="button" class="font-medium underline underline-offset-2" :disabled="disabled" @click="switchToSession">{{ t('governance.sessionImport.switchToSession') }}</button>
    </div>
    <template v-if="form.mode === 'password'">
      <label :for="idPrefix + '-username'" class="block">
        {{ t('governance.username') }}
        <input :id="idPrefix + '-username'" v-model="form.username" class="input w-full" autocomplete="username" required />
      </label>
      <label :for="idPrefix + '-password'" class="block">
        {{ t('governance.password') }}
        <input :id="idPrefix + '-password'" v-model="form.password" class="input w-full" type="text" autocomplete="off" :required="!form.challengeToken" />
      </label>
      <label v-if="challenge === 'totp'" :for="idPrefix + '-otp'" class="block">
        {{ t('governance.otp') }}
        <input :id="idPrefix + '-otp'" v-model="form.otp" class="input w-full" inputmode="numeric" autocomplete="one-time-code" required />
      </label>
    </template>
    <section v-else :aria-labelledby="idPrefix + '-session-import-title'" class="space-y-3">
      <div>
        <h3 :id="idPrefix + '-session-import-title'" class="text-sm font-medium">{{ t('governance.sessionImport.title') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('governance.sessionImport.description') }}</p>
      </div>
      <ol class="list-decimal space-y-1 pl-5 text-sm text-gray-600 dark:text-dark-300">
        <li>
          {{ t('governance.sessionImport.stepLogin') }}
          <a v-if="siteLoginUrl" :id="idPrefix + '-session-import-site'" :href="siteLoginUrl" target="_blank" rel="noopener noreferrer" class="ml-1 text-primary-600 underline underline-offset-2">{{ t('governance.sessionImport.openSite') }}</a>
        </li>
        <li>{{ t('governance.sessionImport.stepCopy') }}</li>
        <li>{{ t('governance.sessionImport.stepPaste') }}</li>
      </ol>
      <p v-if="platform && platform !== 'sub2api'" class="text-xs text-gray-500">{{ t('governance.sessionImport.manualPlatformHint') }}</p>
      <label :for="idPrefix + '-session-import-json'" class="block text-sm">
        {{ t('governance.sessionImport.responseLabel') }}
        <textarea :id="idPrefix + '-session-import-json'" :value="importJson" rows="5" class="input mt-1 w-full resize-y font-mono text-xs" :placeholder="t('governance.sessionImport.responsePlaceholder')" autocomplete="off" autocapitalize="off" :spellcheck="false" :aria-invalid="!!importError" :aria-describedby="importError ? idPrefix + '-session-import-error' : undefined" @input="editImportJson" />
      </label>
      <div class="flex flex-wrap gap-2">
        <button :id="idPrefix + '-session-import-apply'" type="button" class="btn btn-secondary" :disabled="disabled || importReady || !importJson.trim()" @click="applySessionImport">{{ t('governance.sessionImport.importButton') }}</button>
        <button :id="idPrefix + '-session-import-clear'" type="button" class="btn btn-secondary" :disabled="disabled" @click="clearSessionImport">{{ t('governance.sessionImport.clearButton') }}</button>
      </div>
      <p v-if="importError" :id="idPrefix + '-session-import-error'" role="alert" class="text-sm text-red-600">{{ t(`governance.sessionImport.errors.${importError}`) }}</p>
      <p v-if="importReady" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('governance.sessionImport.ready') }}</p>
      <details class="text-xs text-gray-500 dark:text-dark-300">
        <summary class="cursor-pointer">{{ t('governance.sessionRenewal') }}</summary>
        <p class="mt-2 leading-relaxed">{{ t('governance.sessionImport.sessionNotes') }}</p>
      </details>
    </section>
    <button type="button" :id="idPrefix + '-advanced-auth'" class="text-sm text-primary-600 underline" :aria-expanded="advanced" :disabled="disabled" @click="advanced = !advanced">
      {{ t('governance.advancedAuth') }}
    </button>
    <div v-if="advanced" class="space-y-3 border-t pt-3 dark:border-dark-600">
      <p v-if="challenge && challenge !== 'totp' && form.mode === 'password'" class="text-xs text-gray-500">{{ t('governance.sessionImport.description') }}</p>
      <template v-if="form.mode === 'password' && challenge === 'captcha'">
        <label v-if="!form.captchaProvider || form.captchaProvider === 'turnstile'" :for="idPrefix + '-captcha'" class="block">
          {{ t(form.captchaProvider === 'turnstile' ? 'governance.turnstileToken' : 'governance.captchaToken') }}
          <input :id="idPrefix + '-captcha'" v-model="form.captchaToken" class="input w-full" type="text" autocomplete="off" />
        </label>
        <template v-else-if="form.captchaProvider === 'tencent'">
          <label :for="idPrefix + '-tencent-ticket'" class="block">
            {{ t('governance.tencentTicket') }}
            <input :id="idPrefix + '-tencent-ticket'" v-model="form.tencentTicket" class="input w-full" type="text" autocomplete="off" />
          </label>
          <label :for="idPrefix + '-tencent-randstr'" class="block">
            {{ t('governance.tencentRandstr') }}
            <input :id="idPrefix + '-tencent-randstr'" v-model="form.tencentRandstr" class="input w-full" type="text" autocomplete="off" />
          </label>
        </template>
      </template>
      <template v-if="form.mode === 'session'">
        <label :for="idPrefix + '-session'" class="block">
          {{ t('governance.sessionToken') }}
          <input :id="idPrefix + '-session'" :value="form.sessionToken" class="input w-full" type="text" autocomplete="off" required @input="editSessionText('sessionToken', $event)" />
        </label>
        <label :for="idPrefix + '-user-id'" class="block">
          {{ t('governance.userId') }}
          <input :id="idPrefix + '-user-id'" :value="form.userId" class="input w-full" type="number" min="1" @input="editSessionNumber('userId', $event)" />
        </label>
        <label :for="idPrefix + '-refresh-token'" class="block">
          {{ t('governance.sessionRefreshToken') }}
          <input :id="idPrefix + '-refresh-token'" :value="form.refreshToken" class="input w-full" type="text" autocomplete="off" @input="editSessionText('refreshToken', $event)" />
        </label>
        <label :for="idPrefix + '-expires-in'" class="block">
          {{ t('governance.sessionExpiresIn') }}
          <input :id="idPrefix + '-expires-in'" :value="form.expiresIn" class="input w-full" type="number" min="1" step="1" @input="editSessionNumber('expiresIn', $event)" />
        </label>
        <label :for="idPrefix + '-user-agent'" class="block">
          {{ t('governance.sessionUserAgent') }}
          <input :id="idPrefix + '-user-agent'" :value="form.userAgent" class="input w-full" type="text" autocomplete="off" @input="editSessionText('userAgent', $event)" />
        </label>
      </template>
    </div>
  </fieldset>
</template>
