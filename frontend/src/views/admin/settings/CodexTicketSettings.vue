<template>
  <section
    :id="`${id}-settings`"
    class="scroll-mt-40 p-6"
    :aria-labelledby="`${id}-title`"
    :aria-busy="loading || saving"
  >
    <h3 :id="`${id}-title`" class="text-base font-semibold text-gray-900 dark:text-white">
      {{ t(`admin.settings.codexTickets.title${mode}`) }}
    </h3>
    <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.scope') }}</p>
    <div class="mt-4 space-y-4">
      <div class="flex items-center justify-between gap-4">
        <label :for="`${id}-enabled`" class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.codexTickets.enabled') }}
        </label>
        <Toggle :id="`${id}-enabled`" v-model="enabled" :disabled="disabled" :aria-label="t('admin.settings.codexTickets.enabled')" />
      </div>
      <div class="flex items-center justify-between gap-4">
        <div>
          <label :for="`${id}-fail-closed`" class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.codexTickets.failClosed') }}
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.failClosedHint') }}</p>
        </div>
        <Toggle :id="`${id}-fail-closed`" v-model="failClosed" :disabled="disabled" :aria-label="t('admin.settings.codexTickets.failClosed')" />
      </div>
      <div>
        <label :for="`${id}-harvest-proxy`" class="input-label">{{ t('admin.settings.codexTickets.proxy') }}</label>
        <input
          :id="`${id}-harvest-proxy`"
          v-model="proxy"
          :disabled="disabled"
          type="text"
          class="input w-full font-mono text-sm"
          :placeholder="t('admin.settings.codexTickets.proxyPlaceholder')"
          autocomplete="off"
          spellcheck="false"
          @keydown.enter.prevent="save"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.proxyHint') }}</p>
        <p v-if="proxyConfigured" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.proxyConfigured') }}</p>
      </div>
    </div>
    <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(error) }}</p>
      <p v-else role="status" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t(loading ? 'common.loading' : saved ? 'admin.settings.codexTickets.saved' : 'admin.settings.codexTickets.saveHint') }}
      </p>
      <button v-if="!loaded && !loading" type="button" class="btn btn-secondary" @click="load">{{ t('admin.settings.codexTickets.retry') }}</button>
      <button :id="`${id}-save`" type="button" class="btn btn-primary" :disabled="disabled" @click="save">
        {{ t(saving ? 'admin.settings.saving' : 'admin.settings.codexTickets.save', { mode }) }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSettings, updateSettings, type SystemSettings, type UpdateSettingsRequest } from '@/api/admin/settings'
import Toggle from '@/components/common/Toggle.vue'

const props = defineProps<{ mode: '292' | '332' }>()
const { t } = useI18n()
const id = `codex-ticket-${props.mode}`
const keys = props.mode === '292'
  ? { enabled: 'openai_codex_ticket_enabled', failClosed: 'openai_codex_ticket_fail_closed', proxy: 'openai_codex_ticket_harvest_proxy_url', configured: 'openai_codex_ticket_harvest_proxy_configured' } as const
  : { enabled: 'openai_codex_ticket_332_enabled', failClosed: 'openai_codex_ticket_332_fail_closed', proxy: 'openai_codex_ticket_332_harvest_proxy_url', configured: 'openai_codex_ticket_332_harvest_proxy_configured' } as const
const enabled = ref(false)
const failClosed = ref(props.mode === '292')
const proxy = ref('')
const proxyConfigured = ref(false)
const loaded = ref(false)
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const error = ref('')
const disabled = computed(() => !loaded.value || loading.value || saving.value)

watch([enabled, failClosed, proxy], () => {
  saved.value = false
}, { flush: 'sync' })

function apply(settings: SystemSettings) {
  enabled.value = settings[keys.enabled] ?? false
  failClosed.value = settings[keys.failClosed] ?? props.mode === '292'
  proxy.value = settings[keys.proxy] ?? ''
  proxyConfigured.value = settings[keys.configured] ?? false
}

async function load() {
  loading.value = true
  error.value = ''
  saved.value = false
  try {
    apply(await getSettings())
    loaded.value = true
  } catch {
    error.value = 'admin.settings.codexTickets.loadFailed'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (disabled.value) return
  saving.value = true
  saved.value = false
  error.value = ''
  const payload: UpdateSettingsRequest = {
    [keys.enabled]: enabled.value,
    [keys.failClosed]: failClosed.value,
    [keys.proxy]: proxy.value.trim(),
  }
  try {
    apply(await updateSettings(payload))
    saved.value = true
  } catch {
    error.value = 'admin.settings.codexTickets.saveFailed'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
