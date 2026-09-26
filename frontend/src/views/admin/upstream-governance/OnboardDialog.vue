<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConnectionFields from './ConnectionFields.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, continueChallenge, connectionBlocked } from './connection-form'
import { errorKey } from './feedback'
import api, { type Site, type SiteInput, type Snapshot } from '@/api/admin/upstream-governance'

defineProps<{ proxies: { id: number; name: string }[] }>()
const emit = defineEmits<{ close: []; created: [site: Site]; completed: [site: Site, snapshot: Snapshot] }>()
const { t } = useI18n()
const url = ref(''), name = ref(''), platform = ref<SiteInput['platform'] | ''>(''), proxyId = ref<number | null>(null)
const advanced = ref(false), form = ref(newConnectionForm()), savedSite = ref<Site | null>(null)
const connected = ref(false), busy = ref(false), error = ref(''), challenge = ref('')
const stage = ref('')
let disposed = false
onUnmounted(() => { disposed = true; clearConnectionSecrets(form.value) })
function close() {
  clearConnectionSecrets(form.value)
  emit('close')
}
async function onboard() {
  if (busy.value || connectionBlocked(form.value, challenge.value)) return
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
    }
    if (!connected.value) {
      stage.value = 'connecting'
      const result = await api.connect(savedSite.value.id, connectionInput(form.value))
      if (disposed) return
      if (result.challenge) {
        challenge.value = result.challenge.kind
        continueChallenge(form.value, result.challenge)
        stage.value = 'challengePending'
        return
      }
      clearConnectionSecrets(form.value)
      savedSite.value = result.site || { ...savedSite.value, has_credential: true, status: 'connected', last_error: '' }
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
    error.value = t(errorKey(e))
    if (connected.value && (e as { reason?: string }).reason === 'reauth_required') connected.value = false
    const challengeToken = form.value.challengeToken
    clearConnectionSecrets(form.value)
    form.value.challengeToken = challengeToken
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
      <ConnectionFields v-if="!connected" v-model="form" id-prefix="governance-onboard" :challenge="challenge" :disabled="busy" />
      <button id="governance-onboard-advanced" type="button" class="text-sm text-primary-600 underline" :aria-expanded="advanced" :disabled="busy" @click="advanced = !advanced">{{ t('governance.advancedSite') }}</button>
      <fieldset v-if="advanced" :disabled="busy || !!savedSite" class="space-y-3 rounded-lg border p-3 dark:border-dark-600">
        <label class="block" for="governance-onboard-platform">
          {{ t('governance.platform') }}
          <select id="governance-onboard-platform" v-model="platform" class="input w-full">
            <option value="">{{ t('governance.autoDetect') }}</option>
            <option value="sub2api">Sub2API</option><option value="newapi">New API</option>
          </select>
        </label>
        <label class="block" for="governance-onboard-name">{{ t('governance.name') }}<input id="governance-onboard-name" v-model="name" class="input w-full" maxlength="100" :placeholder="t('governance.autoDetect')" /></label>
        <label class="block" for="governance-onboard-proxy">
          {{ t('governance.proxy') }}
          <select id="governance-onboard-proxy" v-model="proxyId" class="input w-full">
            <option :value="null">{{ t('governance.direct') }}</option>
            <option v-for="proxy in proxies" :key="proxy.id" :value="proxy.id">{{ proxy.name }}</option>
          </select>
        </label>
        <p class="text-sm text-gray-500">{{ t('governance.urlHint') }}</p>
      </fieldset>
      <p class="text-xs text-gray-500">{{ t('governance.secretNotice') }}</p>
      <button id="governance-onboard-submit" class="btn btn-primary" :disabled="busy || connectionBlocked(form, challenge)">
        {{ t(busy ? 'common.processing' : connected ? 'governance.retryCollection' : challenge ? 'governance.continueConnection' : 'governance.connectAndCollect') }}
      </button>
    </form>
  </BaseDialog>
</template>
