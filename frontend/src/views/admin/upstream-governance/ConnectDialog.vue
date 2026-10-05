<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ConnectionFields from './ConnectionFields.vue'
import BrowserAuthorizationDialog from './BrowserAuthorizationDialog.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, clearConnectionProofs, continueChallenge, connectionBlocked } from './connection-form'
import { formatGovernanceTime } from './format'
import api, { type AuthorizationStatus, type Site } from '@/api/admin/upstream-governance'
const props = defineProps<{ siteId: number; siteVersion?: number }>()
const emit = defineEmits<{ close: []; connected: [site?: Site] }>()
const { t } = useI18n()
const form = ref(newConnectionForm())
const busy = ref(false), error = ref(''), challenge = ref('')
const loading = ref(true), loadFailed = ref(false), stale = ref(false)
const expectedVersion = ref<number>()
const browserOpen = ref(false), statusLoading = ref(true), statusFailed = ref(false)
const authorizationStatus = ref<AuthorizationStatus | null>(null)
const status = computed(() => authorizationStatus.value?.session.site_version === expectedVersion.value ? authorizationStatus.value : null)
const locked = computed(() => loading.value || busy.value || stale.value || browserOpen.value)
const browserBlocked = computed(() => locked.value || !status.value?.browser.available || !expectedVersion.value || form.value.mode !== 'password' || !form.value.username.trim() || !form.value.password)
const autoReauthorizationLabels: Record<string, string> = {
  ready: 'sessionAutoReauthorizationReady',
  collection_disabled: 'sessionAutoReauthorizationCollectionDisabled',
  missing_session: 'sessionAutoReauthorizationMissingSession',
  missing_credentials: 'sessionAutoReauthorizationMissingCredentials',
  identity_mismatch: 'sessionAutoReauthorizationIdentityMismatch',
  verification_required: 'sessionAutoReauthorizationVerificationRequired',
  credentials_rejected: 'sessionAutoReauthorizationCredentialsRejected',
  retry_wait: 'sessionAutoReauthorizationRetryWait',
}
const autoReauthorizationLabel = computed(() =>
  autoReauthorizationLabels[status.value?.session.auto_reauthorization_state ?? ''] ?? 'sessionAutoReauthorizationUnknown'
)
const renewalLabel = computed(() => {
  const session = status.value?.session
  if (!session?.has_session) return 'sessionMissing'
  if (session.refresh_state === 'pending') return 'sessionRefreshUncertain'
  if (session.refresh_state === 'identity_pending') return 'sessionIdentityPending'
  if (session.reauthorization_required) return session.auto_reauthorization_enabled ? 'sessionReauthorizationScheduled' : 'sessionReauthorizationRequired'
  if (!session.refresh_supported) return 'sessionRefreshUnsupported'
  if (!session.has_refresh_token) return 'sessionRefreshTokenMissing'
  if (session.refresh_state !== 'ready') return 'sessionRefreshUnknown'
  if (!session.expires_at) return 'sessionRefreshTimeUnknown'
  return session.auto_refresh_enabled ? 'sessionRefreshEnabled' : 'sessionRefreshPaused'
})
let disposed = false
let generation = 0
async function loadAuthorizationStatus(id: number, request: number) {
  statusLoading.value = true; statusFailed.value = false; authorizationStatus.value = null
  try {
    const result = await api.authStatus(id)
    if (disposed || request !== generation) return
    if (result.session.site_id !== id || !Number.isSafeInteger(result.session.site_version) || result.session.site_version <= 0) throw new Error('invalid authorization status')
    authorizationStatus.value = result
  } catch {
    if (!disposed && request === generation) statusFailed.value = true
  } finally { if (!disposed && request === generation) statusLoading.value = false }
}
async function loadSavedLogin() {
  if (disposed || busy.value) return
  const request = ++generation, id = props.siteId
  form.value = newConnectionForm()
  challenge.value = ''; error.value = ''; loadFailed.value = false; loading.value = true
  expectedVersion.value = typeof props.siteVersion === 'number' && Number.isSafeInteger(props.siteVersion) && props.siteVersion > 0 ? props.siteVersion : undefined
  void loadAuthorizationStatus(id, request)
  try {
    const credentials = await api.loginCredentials(id)
    if (disposed || request !== generation) return
    if (!Number.isSafeInteger(credentials.version) || credentials.version <= 0) throw { reason: 'invalid_input' }
    form.value.username = credentials.username
    form.value.password = credentials.password
    expectedVersion.value = credentials.version
    stale.value = false
  } catch {
    if (disposed || request !== generation) return
    loadFailed.value = true
    error.value = t(stale.value ? 'governance.loginTargetChanged' : 'governance.loginLoadFailed')
  } finally {
    if (!disposed && request === generation) loading.value = false
  }
}
watch(() => [props.siteId, props.siteVersion], () => {
  busy.value = false; stale.value = false; browserOpen.value = false
  void loadSavedLogin()
}, { immediate: true })
function close() {
  if (busy.value) return
  disposed = true; generation++
  browserOpen.value = false
  form.value = newConnectionForm()
  emit('close')
}
onUnmounted(() => { disposed = true; generation++; form.value = newConnectionForm() })
function invalidateTarget() {
  browserOpen.value = false; stale.value = true
  form.value = newConnectionForm(); challenge.value = ''; expectedVersion.value = undefined
  error.value = t('governance.loginTargetChanged')
}
function openBrowser() {
  if (disposed || browserBlocked.value) return
  clearConnectionProofs(form.value)
  browserOpen.value = true
}
function browserConnected(site?: Site) {
  if (disposed || !browserOpen.value) return
  browserOpen.value = false
  clearConnectionSecrets(form.value)
  emit('connected', site)
}
async function connect() {
  if (disposed || locked.value || connectionBlocked(form.value, challenge.value)) return
  const request = generation, id = props.siteId
  busy.value = true
  error.value = ''
  try {
    const result = await api.connect(id, {
      ...connectionInput(form.value),
      ...(expectedVersion.value != null ? { expected_site_version: expectedVersion.value } : {}),
    })
    if (disposed || request !== generation) return
    if (result.challenge) {
      challenge.value = result.challenge.kind
      continueChallenge(form.value, result.challenge)
    } else {
      clearConnectionSecrets(form.value)
      emit('connected', result.site)
    }
  } catch (e) {
    if (disposed || request !== generation) return
    if ((e as { reason?: string }).reason === 'stale_preview') {
      invalidateTarget()
    } else {
      error.value = t(errorKey(e))
      // Keep edited login details and opaque continuation, but never reuse codes.
      clearConnectionProofs(form.value)
    }
  } finally {
    if (!disposed && request === generation) busy.value = false
  }
}
</script>
<template>
  <BaseDialog :show="!browserOpen" :title="t('governance.connect')" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form class="space-y-4" @submit.prevent="connect">
      <p class="text-sm text-gray-500">{{ t('governance.secretNotice') }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('governance.loadingSavedLogin') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <button v-if="loadFailed || stale" id="governance-connect-reload" type="button" class="btn btn-secondary" :disabled="loading || busy" @click="loadSavedLogin">{{ t('governance.reloadLogin') }}</button>
      <dl v-if="status" class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 border-b pb-3 text-sm dark:border-dark-600">
        <dt class="text-gray-500">{{ t('governance.sessionExpiry') }}</dt><dd class="break-words tabular-nums">{{ status.session.expires_at ? formatGovernanceTime(status.session.expires_at) : t('governance.sessionExpiryUnknown') }}</dd>
        <dt class="text-gray-500">{{ t('governance.sessionRenewal') }}</dt><dd class="break-words">{{ t('governance.' + renewalLabel) }}</dd>
        <dt class="text-gray-500">{{ t('governance.sessionAutoReauthorization') }}</dt>
        <dd class="break-words">
          {{ t('governance.' + autoReauthorizationLabel) }}
          <span v-if="status.session.last_auto_reauthorization_at" class="block text-xs text-gray-500 tabular-nums">{{ t('governance.sessionLastAutoReauthorization') }} {{ formatGovernanceTime(status.session.last_auto_reauthorization_at) }}</span>
        </dd>
      </dl>
      <ConnectionFields :key="siteId" v-model="form" id-prefix="governance-connect" :challenge="challenge" :disabled="locked" />
      <div class="flex flex-wrap gap-2">
        <button id="governance-connect-submit" class="btn btn-primary" :disabled="locked || connectionBlocked(form, challenge)">
          {{ loading ? t('common.loading') : busy ? t('common.processing') : t('governance.connectAndCollect') }}
        </button>
        <button id="governance-connect-browser" type="button" class="btn btn-secondary" :disabled="browserBlocked" @click="openBrowser"><Icon name="globe" size="sm" class="mr-2" />{{ t('governance.browserAuthorization') }}</button>
      </div>
      <p v-if="statusLoading || statusFailed || !status?.browser.available" class="text-xs text-gray-500" role="status">{{ t(statusLoading ? 'common.loading' : statusFailed || !status ? 'governance.authorizationStatusUnavailable' : 'governance.browserUnavailable') }}</p>
    </form>
  </BaseDialog>
  <BrowserAuthorizationDialog v-if="browserOpen && expectedVersion" :site-id="siteId" :site-version="expectedVersion" :username="form.username" :password="form.password" @close="browserOpen = false" @connected="browserConnected" @invalidated="invalidateTarget" />
</template>
