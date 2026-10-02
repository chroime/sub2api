<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ConnectionFields from './ConnectionFields.vue'
import BrowserAuthorizationDialog from './BrowserAuthorizationDialog.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, clearConnectionProofs, continueChallenge, connectionBlocked } from './connection-form'
import { errorKey } from './feedback'
import api, { type AuthorizationStatus, type Site, type SiteInput, type Snapshot } from '@/api/admin/upstream-governance'

const props = defineProps<{ proxies: { id: number; name: string }[] }>()
const emit = defineEmits<{ close: []; created: [site: Site]; completed: [site: Site, snapshot: Snapshot] }>()
const { t } = useI18n()
const url = ref(''), name = ref(''), platform = ref<SiteInput['platform'] | ''>(''), proxyId = ref<number | null>(null)
const advanced = ref(false), form = ref(newConnectionForm()), savedSite = ref<Site | null>(null)
const connected = ref(false), busy = ref(false), error = ref(''), challenge = ref('')
const stage = ref('')
const browserOpen = ref(false), browserStatus = ref<AuthorizationStatus | null>(null), targetStale = ref(false)
const browserBlocked = computed(() => busy.value || targetStale.value || !browserStatus.value?.browser.available || browserStatus.value.session.site_version !== savedSite.value?.version || form.value.mode !== 'password' || !form.value.username.trim() || !form.value.password)
const platformOptions = computed(() => [
  { value: '', label: t('governance.autoDetect') },
  { value: 'sub2api', label: 'Sub2API' },
  { value: 'newapi', label: 'New API' },
])
const proxyOptions = computed(() => [
  { value: null, label: t('governance.direct') },
  ...props.proxies.map(proxy => ({ value: proxy.id, label: proxy.name })),
])
let disposed = false, statusGeneration = 0
onUnmounted(() => { disposed = true; statusGeneration++; clearConnectionSecrets(form.value) })
function close() {
  disposed = true; statusGeneration++; browserOpen.value = false
  clearConnectionSecrets(form.value)
  emit('close')
}
async function loadBrowserStatus(siteId: number) {
  const request = ++statusGeneration
  browserStatus.value = null
  try {
    const status = await api.authStatus(siteId)
    if (!disposed && request === statusGeneration && savedSite.value?.id === siteId && status.session.site_id === siteId && status.session.site_version === savedSite.value.version) browserStatus.value = status
  } catch { /* Browser capability is optional; manual authorization remains available. */ }
}
function openBrowser() {
  if (disposed || browserBlocked.value) return
  clearConnectionProofs(form.value)
  browserOpen.value = true
}
function invalidateTarget() {
  browserOpen.value = false; targetStale.value = true
  clearConnectionSecrets(form.value)
  error.value = t('governance.loginTargetChanged')
}
function browserConnected(site?: Site) {
  if (disposed || !browserOpen.value || !savedSite.value) return
  browserOpen.value = false
  savedSite.value = site || { ...savedSite.value, has_credential: true, status: 'connected', last_error: '' }
  void loadBrowserStatus(savedSite.value.id)
  connected.value = true; challenge.value = ''
  emit('created', savedSite.value)
  void onboard()
}
async function onboard() {
  if (disposed || busy.value || browserOpen.value || targetStale.value || connectionBlocked(form.value, challenge.value)) return
  busy.value = true
  error.value = ''
  try {
    if (!savedSite.value) {
      stage.value = 'detecting'
      let detected
      try {
        detected = await api.detect({ base_url: url.value.trim(), proxy_id: proxyId.value })
      } catch (e) {
        if (!platform.value) throw e
        detected = { base_url: url.value.trim(), name: new URL(url.value.trim()).hostname, platform: platform.value, captcha_required: false }
      }
      if (disposed) return
      stage.value = 'creating'
      savedSite.value = await api.create({
        name: name.value.trim() || detected.name,
        platform: platform.value || detected.platform,
        base_url: detected.base_url,
        proxy_id: proxyId.value,
        enabled: true,
        interval_minutes: 15,
      })
      if (disposed) return
      emit('created', savedSite.value)
      void loadBrowserStatus(savedSite.value.id)
    }
    if (!connected.value) {
      stage.value = 'connecting'
      const result = await api.connect(savedSite.value.id, { ...connectionInput(form.value), expected_site_version: savedSite.value.version })
      if (disposed) return
      if (result.challenge) {
        challenge.value = result.challenge.kind
        continueChallenge(form.value, result.challenge)
        stage.value = 'challengePending'
        return
      }
      savedSite.value = result.site || { ...savedSite.value, has_credential: true, status: 'connected', last_error: '' }
      void loadBrowserStatus(savedSite.value.id)
      emit('created', savedSite.value)
      connected.value = true
      challenge.value = ''
    }
    stage.value = 'collecting'
    const snapshot = await api.sync(savedSite.value.id)
    if (disposed) return
    clearConnectionSecrets(form.value)
    emit('completed', savedSite.value, snapshot)
  } catch (e) {
    if (disposed) return
    if ((e as { reason?: string }).reason === 'stale_preview') { invalidateTarget(); return }
    error.value = t(errorKey(e))
    if (connected.value && (e as { reason?: string }).reason === 'reauth_required') {
      connected.value = false
      if (savedSite.value) {
        const site = savedSite.value
        await loadBrowserStatus(site.id)
        if (disposed || savedSite.value?.id !== site.id) return
        if (form.value.mode === 'password') {
          try {
            const credentials = await api.loginCredentials(site.id)
            if (!disposed && savedSite.value?.id === site.id && credentials.version === site.version && credentials.username && credentials.password) {
              form.value.username = credentials.username
              form.value.password = credentials.password
            }
          } catch { /* The manually entered details remain available. */ }
        }
      }
    }
    clearConnectionProofs(form.value)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :show="!browserOpen" :title="t('governance.add')" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form id="governance-onboard-form" class="space-y-4" @submit.prevent="onboard">
      <p class="text-sm text-gray-500">{{ t('governance.onboardHint') }}</p>
      <p v-if="error" role="alert" class="rounded-lg border border-red-300 p-3 text-red-600">{{ error }}</p>
      <p v-if="stage && !error" role="status" class="text-sm text-primary-600">{{ t('governance.' + stage) }}</p>
      <p v-if="savedSite && !busy" class="text-sm text-gray-500">{{ t('governance.siteSaved', { name: savedSite.name }) }}</p>
      <label for="governance-onboard-url" class="block">
        {{ t('governance.url') }}
        <input id="governance-onboard-url" v-model="url" class="input w-full" type="url" placeholder="https://upstream.example" required :disabled="busy || !!savedSite" />
      </label>
      <ConnectionFields v-if="!connected" v-model="form" id-prefix="governance-onboard" :challenge="challenge" :disabled="busy || browserOpen || targetStale" />
      <button id="governance-onboard-advanced" type="button" class="text-sm text-primary-600 underline" :aria-expanded="advanced" :disabled="busy" @click="advanced = !advanced">{{ t('governance.advancedSite') }}</button>
      <fieldset v-if="advanced" :disabled="busy || !!savedSite" class="space-y-3 rounded-lg border p-3 dark:border-dark-600">
        <div>
          <label class="mb-1 block" for="governance-onboard-platform">{{ t('governance.platform') }}</label>
          <Select id="governance-onboard-platform" v-model="platform" :options="platformOptions" :searchable="false" :disabled="busy || !!savedSite" :aria-label="t('governance.platform')">
            <template #selected="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="server" size="sm" class="shrink-0 text-primary-600" /><span class="truncate">{{ option?.label }}</span></span></template>
            <template #option="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="server" size="sm" class="shrink-0 text-primary-600" /><span class="truncate">{{ option.label }}</span></span></template>
          </Select>
        </div>
        <label class="block" for="governance-onboard-name">{{ t('governance.name') }}<input id="governance-onboard-name" v-model="name" class="input w-full" maxlength="100" :placeholder="t('governance.autoDetect')" /></label>
        <div>
          <label class="mb-1 block" for="governance-onboard-proxy">{{ t('governance.proxy') }}</label>
          <Select id="governance-onboard-proxy" v-model="proxyId" :options="proxyOptions" :disabled="busy || !!savedSite" :aria-label="t('governance.proxy')">
            <template #selected="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="globe" size="sm" class="shrink-0 text-gray-400" /><span class="truncate">{{ option?.label }}</span></span></template>
            <template #option="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="globe" size="sm" class="shrink-0 text-gray-400" /><span class="truncate">{{ option.label }}</span></span></template>
          </Select>
        </div>
        <p class="text-sm text-gray-500">{{ t('governance.urlHint') }}</p>
      </fieldset>
      <p class="text-xs text-gray-500">{{ t('governance.secretNotice') }}</p>
      <div class="flex flex-wrap gap-2">
        <button id="governance-onboard-submit" class="btn btn-primary" :disabled="busy || browserOpen || targetStale || connectionBlocked(form, challenge)">
          {{ t(busy ? 'common.processing' : connected ? 'governance.retryCollection' : challenge ? 'governance.continueConnection' : 'governance.connectAndCollect') }}
        </button>
        <button v-if="savedSite && !connected" id="governance-onboard-browser" type="button" class="btn btn-secondary" :disabled="browserBlocked" @click="openBrowser"><Icon name="globe" size="sm" class="mr-2" />{{ t('governance.browserAuthorization') }}</button>
      </div>
      <p v-if="savedSite && !connected && !browserStatus?.browser.available" class="text-xs text-gray-500">{{ t(browserStatus ? 'governance.browserUnavailable' : 'governance.authorizationStatusUnavailable') }}</p>
    </form>
  </BaseDialog>
  <BrowserAuthorizationDialog v-if="browserOpen && savedSite" :site-id="savedSite.id" :site-version="savedSite.version" :username="form.username" :password="form.password" @close="browserOpen = false" @connected="browserConnected" @invalidated="invalidateTarget" />
</template>
