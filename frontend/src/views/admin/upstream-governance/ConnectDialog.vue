<script setup lang="ts">
import { ref } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import api from '@/api/admin/upstream-governance'
const props = defineProps<{ siteId: number }>()
const emit = defineEmits<{ close: []; connected: [] }>()
const { t } = useI18n()
const mode = ref('password'),
  username = ref(''),
  password = ref(''),
  otp = ref(''),
  challengeToken = ref(''),
  captchaToken = ref(''),
  sessionToken = ref(''),
  userId = ref<number | undefined>()
const busy = ref(false),
  error = ref(''),
  challenge = ref('')
async function connect() {
  busy.value = true
  error.value = ''
  try {
    const result = await api.connect(
      props.siteId,
      mode.value === 'session'
        ? { session_token: sessionToken.value, user_id: userId.value }
        : {
            username: username.value,
            password: password.value,
            otp: otp.value,
            challenge_token: challengeToken.value,
            captcha_token: captchaToken.value,
          },
    )
    password.value = ''
    sessionToken.value = ''
    otp.value = ''
    captchaToken.value = ''
    if (result.challenge) {
      challenge.value = result.challenge.kind
      challengeToken.value = result.challenge.token || ''
    } else {
      challengeToken.value = ''
      emit('connected')
    }
  } catch (e) {
    error.value = t(errorKey(e))
    password.value = ''
    sessionToken.value = ''
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <BaseDialog
    :show="true"
    :title="t('governance.connect')"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="emit('close')"
  >
    <form class="space-y-4" @submit.prevent="connect">
      <p>{{ t('governance.secretNotice') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <p v-if="challenge" role="status" class="text-amber-700">
        {{
          t(challenge === 'totp' ? 'governance.totp' : 'governance.interactive')
        }}
      </p>
      <label class="block"
        >{{ t('governance.authMode')
        }}<select v-model="mode" class="input w-full">
          <option value="password">{{ t('governance.passwordLogin') }}</option>
          <option value="session">{{ t('governance.sessionLogin') }}</option>
        </select></label
      >
      <template v-if="mode === 'password'">
        <label class="block"
          >{{ t('governance.username')
          }}<input
            v-model="username"
            class="input w-full"
            autocomplete="username"
            required
        /></label>
        <label class="block"
          >{{ t('governance.password')
          }}<input
            v-model="password"
            class="input w-full"
            type="password"
            autocomplete="off"
            :required="!challengeToken"
        /></label>
        <label class="block"
          >{{ t('governance.otp')
          }}<input
            v-model="otp"
            class="input w-full"
            autocomplete="one-time-code"
        /></label>
        <label class="block"
          >{{ t('governance.challengeToken')
          }}<input
            v-model="challengeToken"
            class="input w-full"
            type="password"
            autocomplete="off"
        /></label>
        <label class="block"
          >{{ t('governance.captchaToken')
          }}<input
            v-model="captchaToken"
            class="input w-full"
            type="password"
            autocomplete="off"
        /></label>
      </template>
      <template v-else
        ><label class="block"
          >{{ t('governance.sessionToken')
          }}<input
            v-model="sessionToken"
            class="input w-full"
            type="password"
            autocomplete="off"
            required /></label
        ><label class="block"
          >{{ t('governance.userId')
          }}<input
            v-model.number="userId"
            class="input w-full"
            type="number"
            min="1" /></label
      ></template>
      <button class="btn btn-primary" :disabled="busy">
        {{ busy ? t('common.processing') : t('governance.connect') }}
      </button>
    </form>
  </BaseDialog>
</template>
