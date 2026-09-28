<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { clearConnectionSecrets, type ConnectionForm } from './connection-form'

const form = defineModel<ConnectionForm>({ required: true })
defineProps<{ idPrefix: string; challenge?: string; disabled?: boolean }>()
const { t } = useI18n()
const advanced = ref(false)
const modeOptions = computed(() => [
  { value: 'password', label: t('governance.passwordLogin') },
  { value: 'session', label: t('governance.sessionLogin') },
])
watch(() => form.value.mode, () => clearConnectionSecrets(form.value))
</script>

<template>
  <fieldset :disabled="disabled" class="space-y-4">
    <p v-if="challenge" role="status" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
      {{ t(challenge === 'totp' ? 'governance.totp' : challenge === 'captcha' ? 'governance.interactive' : 'governance.unsupportedChallenge') }}
      <span v-if="challenge === 'captcha' && form.captchaProvider" class="mt-1 block font-medium">{{ t(`governance.captchaProvider_${form.captchaProvider}`) }}</span>
      <span v-if="challenge === 'captcha' && (form.captchaProvider === 'aliyun' || form.captchaProvider === 'unknown')" class="mt-1 block">{{ t('governance.captchaBrowserRequired') }}</span>
    </p>
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
      <label v-else-if="challenge === 'captcha' && (!form.captchaProvider || form.captchaProvider === 'turnstile')" :for="idPrefix + '-captcha'" class="block">
        {{ t(form.captchaProvider === 'turnstile' ? 'governance.turnstileToken' : 'governance.captchaToken') }}
        <input :id="idPrefix + '-captcha'" v-model="form.captchaToken" class="input w-full" type="text" autocomplete="off" required />
      </label>
      <template v-else-if="challenge === 'captcha' && form.captchaProvider === 'tencent'">
        <label :for="idPrefix + '-tencent-ticket'" class="block">
          {{ t('governance.tencentTicket') }}
          <input :id="idPrefix + '-tencent-ticket'" v-model="form.tencentTicket" class="input w-full" type="text" autocomplete="off" required />
        </label>
        <label :for="idPrefix + '-tencent-randstr'" class="block">
          {{ t('governance.tencentRandstr') }}
          <input :id="idPrefix + '-tencent-randstr'" v-model="form.tencentRandstr" class="input w-full" type="text" autocomplete="off" required />
        </label>
      </template>
    </template>
    <button type="button" :id="idPrefix + '-advanced-auth'" class="text-sm text-primary-600 underline" :aria-expanded="advanced" @click="advanced = !advanced">
      {{ t('governance.advancedAuth') }}
    </button>
    <div v-if="advanced" class="space-y-3 border-t pt-3 dark:border-dark-600">
      <div>
        <label class="mb-1 block" :for="idPrefix + '-auth-mode'">{{ t('governance.authMode') }}</label>
        <Select :id="idPrefix + '-auth-mode'" v-model="form.mode" :options="modeOptions" :searchable="false" :disabled="disabled" :aria-label="t('governance.authMode')" />
      </div>
    </div>
    <template v-if="form.mode === 'session'">
      <label :for="idPrefix + '-session'" class="block">
        {{ t('governance.sessionToken') }}
        <input :id="idPrefix + '-session'" v-model="form.sessionToken" class="input w-full" type="text" autocomplete="off" required />
      </label>
      <label :for="idPrefix + '-user-id'" class="block">
        {{ t('governance.userId') }}
        <input :id="idPrefix + '-user-id'" v-model.number="form.userId" class="input w-full" type="number" min="1" />
      </label>
      <label :for="idPrefix + '-refresh-token'" class="block">
        {{ t('governance.sessionRefreshToken') }}
        <input :id="idPrefix + '-refresh-token'" v-model="form.refreshToken" class="input w-full" type="text" autocomplete="off" />
      </label>
      <label :for="idPrefix + '-expires-in'" class="block">
        {{ t('governance.sessionExpiresIn') }}
        <input :id="idPrefix + '-expires-in'" v-model.number="form.expiresIn" class="input w-full" type="number" min="1" step="1" />
      </label>
      <label :for="idPrefix + '-user-agent'" class="block">
        {{ t('governance.sessionUserAgent') }}
        <input :id="idPrefix + '-user-agent'" v-model="form.userAgent" class="input w-full" type="text" autocomplete="off" />
      </label>
    </template>
  </fieldset>
</template>
