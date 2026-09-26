<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { clearConnectionSecrets, type ConnectionForm } from './connection-form'

const form = defineModel<ConnectionForm>({ required: true })
defineProps<{ idPrefix: string; challenge?: string; disabled?: boolean }>()
const { t } = useI18n()
const advanced = ref(false)
watch(() => form.value.mode, () => clearConnectionSecrets(form.value))
</script>

<template>
  <fieldset :disabled="disabled" class="space-y-4">
    <p v-if="challenge" role="status" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
      {{ t(challenge === 'totp' ? 'governance.totp' : challenge === 'captcha' ? 'governance.interactive' : 'governance.unsupportedChallenge') }}
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
      <label v-else-if="challenge === 'captcha'" :for="idPrefix + '-captcha'" class="block">
        {{ t('governance.captchaToken') }}
        <input :id="idPrefix + '-captcha'" v-model="form.captchaToken" class="input w-full" type="text" autocomplete="off" required />
      </label>
    </template>
    <button type="button" :id="idPrefix + '-advanced-auth'" class="text-sm text-primary-600 underline" :aria-expanded="advanced" @click="advanced = !advanced">
      {{ t('governance.advancedAuth') }}
    </button>
    <div v-if="advanced" class="space-y-3 rounded-lg border p-3 dark:border-dark-600">
      <p class="text-sm text-gray-500">{{ t('governance.advancedAuthHint') }}</p>
      <label class="block" :for="idPrefix + '-auth-mode'">
        {{ t('governance.authMode') }}
        <select :id="idPrefix + '-auth-mode'" v-model="form.mode" class="input w-full">
          <option value="password">{{ t('governance.passwordLogin') }}</option>
          <option value="session">{{ t('governance.sessionLogin') }}</option>
        </select>
      </label>
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
    </template>
  </fieldset>
</template>
