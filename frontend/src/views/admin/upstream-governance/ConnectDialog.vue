<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConnectionFields from './ConnectionFields.vue'
import { newConnectionForm, connectionInput, clearConnectionSecrets, continueChallenge, connectionBlocked } from './connection-form'
import api, { type Site } from '@/api/admin/upstream-governance'
const props = defineProps<{ siteId: number }>()
const emit = defineEmits<{ close: []; connected: [site?: Site] }>()
const { t } = useI18n()
const form = ref(newConnectionForm())
const busy = ref(false), error = ref(''), challenge = ref('')
let disposed = false
function close() {
  clearConnectionSecrets(form.value)
  emit('close')
}
onUnmounted(() => { disposed = true; clearConnectionSecrets(form.value) })
async function connect() {
  if (busy.value || connectionBlocked(form.value, challenge.value)) return
  busy.value = true
  error.value = ''
  try {
    const result = await api.connect(props.siteId, connectionInput(form.value))
    if (disposed) return
    if (result.challenge) {
      challenge.value = result.challenge.kind
      continueChallenge(form.value, result.challenge)
    } else {
      clearConnectionSecrets(form.value)
      emit('connected', result.site)
    }
  } catch (e) {
    if (!disposed) error.value = t(errorKey(e))
    // Preserve an opaque challenge for another one-time-code attempt.
    const challengeToken = form.value.challengeToken
    clearConnectionSecrets(form.value)
    if (!disposed) form.value.challengeToken = challengeToken
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <BaseDialog :show="true" :title="t('governance.connect')" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form class="space-y-4" @submit.prevent="connect">
      <p class="text-sm text-gray-500">{{ t('governance.secretNotice') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <ConnectionFields v-model="form" id-prefix="governance-connect" :challenge="challenge" :disabled="busy" />
      <button id="governance-connect-submit" class="btn btn-primary" :disabled="busy || connectionBlocked(form, challenge)">
        {{ busy ? t('common.processing') : t('governance.connectAndCollect') }}
      </button>
    </form>
  </BaseDialog>
</template>
