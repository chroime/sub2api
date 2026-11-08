<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamPlatformIcon from './UpstreamPlatformIcon.vue'
import ConnectionFields from './ConnectionFields.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, clearConnectionProofs, continueChallenge, connectionBlocked } from './connection-form'
import { errorKey } from './feedback'
import api, { type Site, type SiteInput, type Snapshot } from '@/api/admin/upstream-governance'

const props = defineProps<{ proxies: { id: number; name: string }[] }>()
const emit = defineEmits<{ close: []; created: [site: Site]; completed: [site: Site, snapshot: Snapshot] }>()
const { t } = useI18n()
const url = ref(''), name = ref(''), platform = ref<SiteInput['platform'] | ''>(''), proxyId = ref<number | null>(null)
const advanced = ref(false), form = ref(newConnectionForm()), savedSite = ref<Site | null>(null)
const connected = ref(false), busy = ref(false), error = ref(''), challenge = ref('')
const stage = ref('')
const targetStale = ref(false)
const platformOptions = computed(() => [
  { value: '', label: t('governance.autoDetect') },
  { value: 'sub2api', label: 'Sub2API' },
  { value: 'newapi', label: 'New API' },
])
const proxyOptions = computed(() => [
  { value: null, label: t('governance.direct') },
  ...props.proxies.map(proxy => ({ value: proxy.id, label: proxy.name })),
])
let disposed = false
onUnmounted(() => { disposed = true; clearConnectionSecrets(form.value) })
function close() {
  disposed = true
  clearConnectionSecrets(form.value)
  emit('close')
}
function invalidateTarget() {
  targetStale.value = true
  clearConnectionSecrets(form.value)
  error.value = t('governance.loginTargetChanged')
}
async function onboard() {
  if (disposed || busy.value || targetStale.value || (!connected.value && connectionBlocked(form.value, challenge.value))) return
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
      const defaults = await api.globalSettings().catch(() => null)
      savedSite.value = await api.create({
        name: name.value.trim() || detected.name,
        platform: platform.value || detected.platform,
        base_url: detected.base_url,
        proxy_id: proxyId.value,
        enabled: true,
        interval_minutes: 15,
        fast_interval_seconds: defaults?.defaults.fast_interval_seconds ?? 60,
        full_interval_seconds: defaults?.defaults.full_interval_seconds ?? 900,
      })
      if (disposed) return
      emit('created', savedSite.value)
      if (form.value.mode === 'password' && detected.captcha_required === true) {
        challenge.value = 'captcha'
        continueChallenge(form.value, { kind: 'captcha', provider: detected.captcha_provider })
        stage.value = 'challengePending'
        return
      }
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
      emit('created', savedSite.value)
      connected.value = true
      challenge.value = ''
      clearConnectionSecrets(form.value)
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
  <BaseDialog :show="true" :title="t('governance.add')" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form id="governance-onboard-form" class="space-y-4" @submit.prevent="onboard">
      <p class="text-sm text-gray-500">{{ t('governance.onboardHint') }}</p>
      <p v-if="error" role="alert" class="rounded-lg border border-red-300 p-3 text-red-600">{{ error }}</p>
      <p v-if="stage && !error" role="status" class="text-sm text-primary-600">{{ t('governance.' + stage) }}</p>
      <p v-if="savedSite && !busy" class="text-sm text-gray-500">{{ t('governance.siteSaved', { name: savedSite.name }) }}</p>
      <label for="governance-onboard-url" class="block">
        {{ t('governance.url') }}
        <input id="governance-onboard-url" v-model="url" class="input w-full" type="url" placeholder="https://upstream.example" required :disabled="busy || !!savedSite" />
      </label>
      <ConnectionFields v-if="!connected" v-model="form" id-prefix="governance-onboard" :challenge="challenge" :disabled="busy || targetStale" :site-url="savedSite?.base_url || url" :platform="savedSite?.platform || platform || undefined" />
      <button id="governance-onboard-advanced" type="button" class="text-sm text-primary-600 underline" :aria-expanded="advanced" :disabled="busy" @click="advanced = !advanced">{{ t('governance.advancedSite') }}</button>
      <fieldset v-if="advanced" :disabled="busy || !!savedSite" class="space-y-3 rounded-lg border p-3 dark:border-dark-600">
        <div>
          <label class="mb-1 block" for="governance-onboard-platform">{{ t('governance.platform') }}</label>
          <Select id="governance-onboard-platform" v-model="platform" :options="platformOptions" :searchable="false" :disabled="busy || !!savedSite" :aria-label="t('governance.platform')">
            <template #selected="{ option }"><span class="flex min-w-0 items-center gap-2"><UpstreamPlatformIcon :platform="String(option?.value ?? '')" /><span class="truncate">{{ option?.label }}</span></span></template>
            <template #option="{ option }"><span class="flex min-w-0 items-center gap-2"><UpstreamPlatformIcon :platform="String(option.value ?? '')" /><span class="truncate">{{ option.label }}</span></span></template>
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
        <button id="governance-onboard-submit" class="btn btn-primary" :disabled="busy || targetStale || (!connected && connectionBlocked(form, challenge))">
          {{ t(busy ? 'common.processing' : connected ? 'governance.retryCollection' : form.mode === 'session' ? 'governance.importSessionAndCollect' : challenge ? 'governance.continueConnection' : 'governance.connectAndCollect') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
