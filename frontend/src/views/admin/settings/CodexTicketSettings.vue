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
    <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`admin.settings.codexTickets.description${mode}`) }}</p>
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
      <div class="flex items-center justify-between gap-4">
        <div>
          <label :for="`${id}-verify`" class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.codexTickets.verify') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.verifyHint') }}</p>
        </div>
        <Toggle :id="`${id}-verify`" v-model="verifyEnabled" :disabled="disabled" :aria-label="t('admin.settings.codexTickets.verify')" />
      </div>
      <div>
        <label :for="`${id}-concurrency`" class="input-label">{{ t('admin.settings.codexTickets.concurrency') }}</label>
        <input :id="`${id}-concurrency`" v-model.number="concurrency" type="number" min="1" max="16" step="1" class="input max-w-40" :disabled="disabled" :aria-invalid="!validConcurrency" @keydown.enter.prevent="save" />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.concurrencyHint') }}</p>
        <p v-if="loaded && !validConcurrency" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ t('admin.settings.codexTickets.invalidConcurrency') }}</p>
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
      <fieldset :disabled="disabled" class="space-y-2">
        <legend class="input-label">{{ t('admin.settings.codexTickets.proxyPool') }}</legend>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.proxyPoolHint') }}</p>
        <input :id="`${id}-proxy-search`" v-model="proxySearch" type="search" class="input" :aria-label="t('admin.settings.codexTickets.proxySearch')" :placeholder="t('admin.settings.codexTickets.proxySearch')" />
        <div v-if="proxyOptions.length" class="max-h-48 overflow-y-auto rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <label v-for="option in filteredProxies" :key="option.id" class="flex cursor-pointer items-center gap-3 py-2 text-sm text-gray-700 dark:text-gray-300">
            <input :id="`${id}-proxy-${option.id}`" v-model="proxyIds" type="checkbox" :value="option.id" :disabled="disabled || (proxyIds.length >= 32 && !proxyIds.includes(option.id))" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 break-words">{{ option.name }} <span class="text-xs text-gray-500">#{{ option.id }}<template v-if="option.protocol"> · {{ option.protocol.toUpperCase() }}</template></span></span>
          </label>
          <p v-if="!filteredProxies.length" class="text-xs text-gray-500">{{ t('admin.settings.codexTickets.proxyNoMatches') }}</p>
        </div>
        <p v-else class="text-xs text-gray-500">{{ t(proxiesLoading ? 'common.loading' : 'admin.settings.codexTickets.proxyPoolEmpty') }}</p>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.proxySelected', { count: proxyIds.length }) }}</p>
        <p v-if="proxiesError" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t('admin.settings.codexTickets.proxyLoadFailed') }}</p>
        <button :id="`${id}-proxy-reload`" type="button" class="btn btn-secondary btn-sm" :disabled="proxiesLoading || disabled" @click="loadProxies">{{ t('admin.settings.codexTickets.proxyReload') }}</button>
      </fieldset>
    </div>
    <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(error) }}</p>
      <p v-else role="status" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t(loading ? 'common.loading' : saved ? 'admin.settings.codexTickets.saved' : 'admin.settings.codexTickets.saveHint') }}
      </p>
      <button v-if="!loaded && !loading" type="button" class="btn btn-secondary" @click="load">{{ t('admin.settings.codexTickets.retry') }}</button>
      <button :id="`${id}-save`" type="button" class="btn btn-primary" :disabled="disabled || !validConcurrency || proxyIds.length > 32" @click="save">
        {{ t(saving ? 'admin.settings.saving' : 'admin.settings.codexTickets.save', { mode }) }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSettings, updateSettings, type SystemSettings, type UpdateSettingsRequest } from '@/api/admin/settings'
import { getAll as getProxies } from '@/api/admin/proxies'
import Toggle from '@/components/common/Toggle.vue'

const props = defineProps<{ mode: '292' | '332' }>()
const { t } = useI18n()
const id = `codex-ticket-${props.mode}`
const keys = props.mode === '292'
  ? { enabled: 'openai_codex_ticket_enabled', failClosed: 'openai_codex_ticket_fail_closed', proxy: 'openai_codex_ticket_harvest_proxy_url', configured: 'openai_codex_ticket_harvest_proxy_configured', verify: 'openai_codex_ticket_verify_enabled', proxyIds: 'openai_codex_ticket_harvest_proxy_ids', concurrency: 'openai_codex_ticket_harvest_concurrency' } as const
  : { enabled: 'openai_codex_ticket_332_enabled', failClosed: 'openai_codex_ticket_332_fail_closed', proxy: 'openai_codex_ticket_332_harvest_proxy_url', configured: 'openai_codex_ticket_332_harvest_proxy_configured', verify: 'openai_codex_ticket_332_verify_enabled', proxyIds: 'openai_codex_ticket_332_harvest_proxy_ids', concurrency: 'openai_codex_ticket_332_harvest_concurrency' } as const
const enabled = ref(false)
const failClosed = ref(props.mode === '292')
const proxy = ref('')
const proxyConfigured = ref(false)
const verifyEnabled = ref(false)
const concurrency = ref<number | string>(3)
const proxyIds = ref<number[]>([])
const proxySearch = ref('')
const proxies = ref<Array<{ id: number; name: string; protocol: string }>>([])
const proxiesLoading = ref(false)
const proxiesError = ref(false)
const loaded = ref(false)
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const error = ref('')
const disabled = computed(() => !loaded.value || loading.value || saving.value)
const validConcurrency = computed(() => typeof concurrency.value === 'number' && Number.isInteger(concurrency.value) && concurrency.value >= 1 && concurrency.value <= 16)
const proxyOptions = computed(() => {
  const known = new Set(proxies.value.map(item => item.id))
  return [...proxies.value, ...proxyIds.value.filter(proxyId => !known.has(proxyId)).map(proxyId => ({ id: proxyId, name: t('admin.settings.codexTickets.proxyUnavailable'), protocol: '' }))]
})
const filteredProxies = computed(() => {
  const query = proxySearch.value.trim().toLocaleLowerCase()
  return proxyOptions.value.filter(item => `${item.name} ${item.id}`.toLocaleLowerCase().includes(query))
})

watch([enabled, failClosed, proxy, verifyEnabled, concurrency, proxyIds], () => {
  saved.value = false
}, { flush: 'sync' })

function apply(settings: SystemSettings) {
  enabled.value = settings[keys.enabled] ?? false
  failClosed.value = settings[keys.failClosed] ?? props.mode === '292'
  proxy.value = settings[keys.proxy] ?? ''
  proxyConfigured.value = settings[keys.configured] ?? false
  verifyEnabled.value = settings[keys.verify] ?? false
  concurrency.value = settings[keys.concurrency] ?? 3
  proxyIds.value = [...(settings[keys.proxyIds] ?? [])]
}

async function loadProxies() {
  if (proxiesLoading.value) return
  proxiesLoading.value = true
  proxiesError.value = false
  try {
    const now = Date.now()
    proxies.value = (await getProxies())
      .filter(item => item.status === 'active' && (!item.expires_at || Date.parse(item.expires_at) > now))
      .map(item => ({ id: item.id, name: item.name, protocol: item.protocol }))
  } catch {
    proxiesError.value = true
  } finally {
    proxiesLoading.value = false
  }
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
  if (disabled.value || !validConcurrency.value || proxyIds.value.length > 32) return
  saving.value = true
  saved.value = false
  error.value = ''
  const payload: UpdateSettingsRequest = {
    [keys.enabled]: enabled.value,
    [keys.failClosed]: failClosed.value,
    [keys.proxy]: proxy.value.trim(),
    [keys.verify]: verifyEnabled.value,
    [keys.proxyIds]: [...proxyIds.value],
    [keys.concurrency]: Number(concurrency.value),
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

onMounted(() => {
  void load()
  void loadProxies()
})
</script>
