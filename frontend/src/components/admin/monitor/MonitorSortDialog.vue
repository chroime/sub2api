<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.sort.title')"
    width="normal"
    :close-on-escape="!saving"
    :show-close-button="!saving"
    @close="close"
  >
    <div class="space-y-4" :aria-busy="loading || saving">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.channelMonitor.sort.hint') }}
      </p>

      <p v-if="loading" class="py-8 text-center text-sm text-gray-500" role="status">
        {{ t('common.loading') }}
      </p>
      <div v-else-if="loadFailed" class="space-y-3 py-6 text-center" role="alert">
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('admin.channelMonitor.sort.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadItems">{{ t('common.refresh') }}</button>
      </div>
      <p v-else-if="items.length === 0" class="py-8 text-center text-sm text-gray-500">
        {{ t('admin.channelMonitor.sort.empty') }}
      </p>
      <VueDraggable
        v-else
        v-model="items"
        :animation="200"
        :disabled="saving"
        handle=".monitor-sort-handle"
        class="space-y-2"
      >
        <div
          v-for="(item, index) in items"
          :key="item.id"
          :data-monitor-sort-id="item.id"
          class="flex items-center gap-3 rounded-lg border border-gray-200 bg-white p-3 transition-shadow hover:shadow-md dark:border-dark-600 dark:bg-dark-700"
        >
          <span class="monitor-sort-handle cursor-grab text-gray-400 active:cursor-grabbing" aria-hidden="true">
            <Icon name="menu" size="md" />
          </span>
          <div class="min-w-0 flex-1">
            <div class="truncate font-medium text-gray-900 dark:text-white" :title="item.name">{{ item.name }}</div>
            <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span class="rounded-md px-2 py-0.5" :class="providerBadgeClass(item.provider)">{{ providerLabel(item.provider) }}</span>
              <span v-if="!item.enabled">{{ t('common.disabled') }}</span>
              <span>#{{ item.id }}</span>
            </div>
          </div>
          <div class="flex shrink-0 gap-1">
            <button
              type="button"
              class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-30 dark:text-gray-400 dark:hover:bg-dark-600"
              :disabled="saving || index === 0"
              :aria-label="t('admin.channelMonitor.sort.moveUp', { name: item.name })"
              @click="move(index, -1)"
            >
              <Icon name="chevronUp" size="sm" />
            </button>
            <button
              type="button"
              class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-30 dark:text-gray-400 dark:hover:bg-dark-600"
              :disabled="saving || index === items.length - 1"
              :aria-label="t('admin.channelMonitor.sort.moveDown', { name: item.name })"
              @click="move(index, 1)"
            >
              <Icon name="chevronDown" size="sm" />
            </button>
          </div>
        </div>
      </VueDraggable>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSave" @click="save">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { adminAPI } from '@/api/admin'
import type { ChannelMonitorSortItem } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved'): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const { providerLabel, providerBadgeClass } = useChannelMonitorFormat()
const items = ref<ChannelMonitorSortItem[]>([])
const loading = ref(false)
const saving = ref(false)
const loadFailed = ref(false)
let controller: AbortController | null = null
const canSave = computed(() => !loading.value && !saving.value && !loadFailed.value && items.value.length > 0)

async function loadItems() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  loadFailed.value = false
  items.value = []
  try {
    const result = await adminAPI.channelMonitor.getSortOrder({ signal: request.signal })
    if (!request.signal.aborted) items.value = result
  } catch (error) {
    if (request.signal.aborted) return
    loadFailed.value = true
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.sort.loadFailed')))
  } finally {
    if (controller === request) loading.value = false
  }
}

function move(index: number, offset: number) {
  const destination = index + offset
  if (saving.value || destination < 0 || destination >= items.value.length) return
  const next = [...items.value]
  const [item] = next.splice(index, 1)
  next.splice(destination, 0, item)
  items.value = next
}

function close() {
  if (!saving.value) emit('close')
}

async function save() {
  if (!canSave.value) return
  saving.value = true
  try {
    await adminAPI.channelMonitor.updateSortOrder(items.value.map((item, index) => ({
      id: item.id,
      sort_order: index * 10,
    })))
    appStore.showSuccess(t('admin.channelMonitor.sort.success'))
    emit('saved')
    emit('close')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.sort.saveFailed')))
  } finally {
    saving.value = false
  }
}

watch(() => props.show, show => {
  if (show) {
    void loadItems()
  } else {
    controller?.abort()
    controller = null
    items.value = []
  }
}, { immediate: true })

onUnmounted(() => controller?.abort())
</script>
