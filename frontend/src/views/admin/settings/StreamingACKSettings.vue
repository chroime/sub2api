<template>
  <section
    id="streaming-ack-settings"
    class="card scroll-mt-40 p-6"
    aria-labelledby="streaming-ack-title"
    :aria-busy="loading || saving"
  >
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <h4 id="streaming-ack-title" class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.streamingACK.title') }}
        </h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.streamingACK.scope') }}
        </p>
      </div>
      <Toggle
        :model-value="enabled"
        :disabled="!loaded || loading || saving"
        :aria-label="t('admin.settings.streamingACK.title')"
        class="disabled:cursor-not-allowed disabled:opacity-50"
        data-testid="streaming-ack-master-toggle"
        @update:model-value="save"
      />
    </div>
    <div class="mt-2 flex min-h-5 items-center gap-2 text-xs">
      <p v-if="error" role="alert" class="text-red-600 dark:text-red-400">{{ t(error) }}</p>
      <p v-else role="status" class="text-gray-500 dark:text-gray-400">{{ t(statusKey) }}</p>
      <button
        v-if="error && !loaded"
        type="button"
        data-testid="streaming-ack-retry"
        class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded text-gray-600 hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:text-gray-300 dark:hover:bg-dark-700"
        :disabled="loading"
        :aria-label="t('admin.settings.streamingACK.retry')"
        :title="t('admin.settings.streamingACK.retry')"
        @click="load"
      >
        <Icon name="refresh" size="sm" aria-hidden="true" />
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStreamingACKSettings, updateStreamingACKSettings } from '@/api/admin/settings'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const enabled = ref(false)
const loaded = ref(false)
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const error = ref('')
const statusKey = computed(() => {
  if (loading.value) return 'common.loading'
  if (saving.value) return 'admin.settings.saving'
  if (saved.value) return 'admin.settings.streamingACK.saved'
  return enabled.value ? 'admin.settings.streamingACK.enabled' : 'admin.settings.streamingACK.disabled'
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const settings = await getStreamingACKSettings()
    enabled.value = settings.enabled
    loaded.value = true
  } catch {
    error.value = 'admin.settings.streamingACK.loadFailed'
  } finally {
    loading.value = false
  }
}

async function save(value: boolean) {
  if (!loaded.value || loading.value || saving.value) return
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    const settings = await updateStreamingACKSettings({ enabled: value })
    enabled.value = settings.enabled
    saved.value = true
  } catch {
    loaded.value = false
    error.value = 'admin.settings.streamingACK.saveFailed'
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void load()
  if (window.location.hash === '#streaming-ack-settings') {
    window.requestAnimationFrame(() => {
      document.getElementById('streaming-ack-settings')?.scrollIntoView({ block: 'center' })
    })
  }
})
</script>
