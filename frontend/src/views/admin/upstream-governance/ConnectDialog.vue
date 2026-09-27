<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConnectionFields from './ConnectionFields.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, continueChallenge, connectionBlocked } from './connection-form'
import api, { type Site } from '@/api/admin/upstream-governance'
const props = defineProps<{ siteId: number; siteVersion?: number }>()
const emit = defineEmits<{ close: []; connected: [site?: Site] }>()
const { t } = useI18n()
const form = ref(newConnectionForm())
const busy = ref(false), error = ref(''), challenge = ref('')
const loading = ref(true), loadFailed = ref(false), stale = ref(false)
const expectedVersion = ref<number>()
const locked = computed(() => loading.value || busy.value || stale.value)
let disposed = false
let generation = 0
async function loadSavedLogin() {
  if (disposed || busy.value) return
  const request = ++generation, id = props.siteId
  form.value = newConnectionForm()
  challenge.value = ''; error.value = ''; loadFailed.value = false; loading.value = true
  expectedVersion.value = typeof props.siteVersion === 'number' && Number.isSafeInteger(props.siteVersion) && props.siteVersion > 0 ? props.siteVersion : undefined
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
watch(() => props.siteId, () => {
  busy.value = false; stale.value = false
  void loadSavedLogin()
}, { immediate: true })
function close() {
  if (busy.value) return
  disposed = true; generation++
  form.value = newConnectionForm()
  emit('close')
}
onUnmounted(() => { disposed = true; generation++; form.value = newConnectionForm() })
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
      stale.value = true
      form.value = newConnectionForm(); challenge.value = ''; expectedVersion.value = undefined
      error.value = t('governance.loginTargetChanged')
    } else {
      error.value = t(errorKey(e))
      // Keep edited login details and opaque continuation, but never reuse codes.
      form.value.otp = ''; form.value.captchaToken = ''
    }
  } finally {
    if (!disposed && request === generation) busy.value = false
  }
}
</script>
<template>
  <BaseDialog :show="true" :title="t('governance.connect')" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form class="space-y-4" @submit.prevent="connect">
      <p class="text-sm text-gray-500">{{ t('governance.secretNotice') }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('governance.loadingSavedLogin') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <button v-if="loadFailed || stale" id="governance-connect-reload" type="button" class="btn btn-secondary" :disabled="loading || busy" @click="loadSavedLogin">{{ t('governance.reloadLogin') }}</button>
      <ConnectionFields :key="siteId" v-model="form" id-prefix="governance-connect" :challenge="challenge" :disabled="locked" />
      <button id="governance-connect-submit" class="btn btn-primary" :disabled="locked || connectionBlocked(form, challenge)">
        {{ loading ? t('common.loading') : busy ? t('common.processing') : t('governance.connectAndCollect') }}
      </button>
    </form>
  </BaseDialog>
</template>
