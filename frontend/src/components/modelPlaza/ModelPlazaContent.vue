<template>
  <div class="model-plaza-content space-y-6">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div v-if="!embedded">
      <h1 class="text-3xl font-bold tracking-tight plaza-heading text-gray-900 dark:text-white sm:text-4xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-2 text-base leading-relaxed plaza-muted text-gray-500 dark:text-dark-400">{{ t('modelPlaza.description') }}</p>
    </div>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <details v-if="descriptionHtml" class="plaza-description rounded-xl border border-gray-100 bg-white px-4 py-3 text-sm dark:border-dark-700 dark:bg-dark-800/50">
      <summary class="cursor-pointer font-medium text-gray-600 dark:text-dark-200">{{ t('modelPlaza.filters.description') }}</summary>
      <div class="mt-3" v-html="descriptionHtml"></div>
    </details>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="flex items-center gap-2 text-sm leading-relaxed plaza-soft text-gray-400 dark:text-dark-500"
    >
      <Icon name="infoCircle" size="sm" class="h-4 w-4 shrink-0" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="plaza-loading-spinner h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="plaza-error rounded-2xl border border-red-200 bg-red-50 px-5 py-8 text-center text-base text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <div
      v-else-if="groups.length === 0"
      class="plaza-empty rounded-2xl border border-dashed border-gray-300 px-5 py-12 text-center text-base plaza-muted text-gray-500 dark:border-dark-600 dark:text-dark-400"
    >
      {{ t('modelPlaza.empty') }}
    </div>

    <!-- Price view, access group, vendor filters and model table. -->
    <div v-else class="space-y-4">
      <section class="min-w-0 space-y-4">
        <div class="plaza-toolbar overflow-hidden rounded-2xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800" data-plaza-toolbar>
        <div class="flex flex-wrap items-center gap-3 p-4">
          <span class="shrink-0 text-sm font-semibold text-gray-700 dark:text-dark-200">{{ t('modelPlaza.filters.priceView') }}</span>
          <div class="plaza-price-views inline-flex min-w-0 rounded-xl bg-gray-100 p-1 dark:bg-dark-900/70" role="group" :aria-label="t('modelPlaza.filters.priceView')">
            <button
              v-for="view in priceViews" :key="view" type="button" :data-price-view="view" :aria-pressed="priceView === view"
              class="min-h-10 rounded-lg px-3 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500"
              :class="priceView === view ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
              @click="selectPriceView(view)"
            >{{ t(view === 'official' ? 'modelPlaza.filters.officialScope' : 'modelPlaza.priceTable.sitePrice') }}</button>
          </div>
          <label class="plaza-model-search relative w-full min-w-0 sm:ml-auto sm:w-auto sm:min-w-[220px] sm:flex-1">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="searchQuery" type="search" class="input plaza-search w-full py-2.5 pl-9 text-sm" :placeholder="t('modelPlaza.filters.searchPlaceholder')" :aria-label="t('modelPlaza.filters.searchPlaceholder')" />
          </label>
          <button v-if="searchActive || selectedVendor !== 'all'" type="button" data-clear-plaza-filters class="text-xs text-gray-500 hover:text-primary-600" @click="clearFilters">{{ t('modelPlaza.filters.clear') }}</button>
        </div>
        <div v-if="priceView === 'site'" class="plaza-access-group flex flex-wrap items-center gap-3 border-t border-gray-200 p-4 dark:border-dark-700">
          <label for="plaza-group-select" class="shrink-0 text-sm font-semibold text-gray-700 dark:text-dark-200">
            {{ t('modelPlaza.filters.accessGroup') }}
          </label>
          <div class="plaza-group-select min-w-0 w-full sm:w-80 lg:w-[360px]">
            <Select
              id="plaza-group-select"
              :model-value="selectedGroupId"
              :options="groupSelectOptions"
              :aria-label="t('modelPlaza.filters.accessGroup')"
              :search-placeholder="t('modelPlaza.filters.searchGroups')"
              :dropdown-width="600"
              interactive-options
              @update:model-value="onGroupChange"
              @open-change="groupPickerOpen = $event"
            >
              <template #options-header><GroupMonitorHeader /></template>
              <template #selected>
                <GroupBadge
                  v-if="selectedGroup"
                  :name="selectedGroup.name"
                  :identity="selectedGroup.identity"
                  :platform="selectedGroup.platform as GroupPlatform"
                  :subscription-type="selectedGroup.subscription_type as SubscriptionType"
                  :rate-multiplier="selectedGroup.rate_multiplier"
                  :user-rate-multiplier="selectedGroup.user_rate_multiplier"
                  :peak-rate-enabled="selectedGroup.peak_rate_enabled"
                  :peak-start="selectedGroup.peak_start"
                  :peak-end="selectedGroup.peak_end"
                  :peak-rate-multiplier="selectedGroup.peak_rate_multiplier"
                  always-show-rate
                  class="max-w-full"
                />
              </template>
              <template #option="{ option, selected }">
                <GroupOptionItem
                  v-if="(option as PlazaGroupSelectOption).group"
                  :name="(option as PlazaGroupSelectOption).group!.name"
                  :identity="(option as PlazaGroupSelectOption).group!.identity"
                  :platform="(option as PlazaGroupSelectOption).group!.platform as GroupPlatform"
                  :subscription-type="(option as PlazaGroupSelectOption).group!.subscription_type as SubscriptionType"
                  :rate-multiplier="(option as PlazaGroupSelectOption).group!.rate_multiplier"
                  :user-rate-multiplier="(option as PlazaGroupSelectOption).group!.user_rate_multiplier"
                  :peak-rate-enabled="(option as PlazaGroupSelectOption).group!.peak_rate_enabled"
                  :peak-start="(option as PlazaGroupSelectOption).group!.peak_start"
                  :peak-end="(option as PlazaGroupSelectOption).group!.peak_end"
                  :peak-rate-multiplier="(option as PlazaGroupSelectOption).group!.peak_rate_multiplier"
                  :description="(option as PlazaGroupSelectOption).group!.description"
                  :selected="selected"
                  :monitoring="groupMonitorStates.get((option as PlazaGroupSelectOption).group!.id)"
                />
                <template v-else>
                  <span class="min-w-0 flex-1 truncate text-left text-[15px] leading-6">{{ option.label }}</span>
                  <Icon v-if="selected" name="check" size="sm" class="shrink-0 text-primary-500" :stroke-width="2" />
                </template>
              </template>
            </Select>
          </div>
        </div>
        <PlazaVendorFilter :vendors="scopeVendors" :vendor="selectedVendor" @update:vendor="selectVendor" />
        </div>
        <p data-price-view-hint class="text-xs leading-relaxed text-gray-500 dark:text-dark-300">{{ t(priceView === 'official' ? 'modelPlaza.filters.officialViewHint' : 'modelPlaza.filters.siteViewHint') }}</p>

        <p
          v-if="selectedGroup?.description"
          class="text-[15px] leading-relaxed plaza-muted text-gray-500 dark:text-dark-400"
        >
          {{ selectedGroup.description }}
        </p>
        <p
          v-if="peakNote"
          class="inline-flex items-center gap-1.5 text-sm leading-relaxed plaza-warning text-amber-600 dark:text-amber-400"
        >
          <Icon name="clock" size="sm" class="h-4 w-4 shrink-0" />
          {{ peakNote }}
        </p>

        <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ currentVendorLabel }}</h2>

        <PlazaVendorPriceTable
          v-if="tableModels.length > 0"
          :models="tableModels"
          :group="selectedGroup"
          :caption="currentVendorLabel"
          :highlighted="highlightedKey"
          :peak-window="peakWindow"
        />
        <div
          v-else
          class="plaza-empty rounded-2xl border border-dashed border-gray-300 px-5 py-12 text-center text-base plaza-muted text-gray-500 dark:border-dark-600 dark:text-dark-400"
        >
          <p>{{ emptyMessage }}</p>
          <button v-if="searchActive || selectedVendor !== 'all'" type="button" data-empty-clear class="mt-3 text-sm font-medium text-primary-600 dark:text-primary-400" @click="clearFilters">{{ t(selectedGroup ? 'modelPlaza.filters.showGroupModels' : 'modelPlaza.filters.showCatalogModels') }}</button>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import GroupMonitorHeader from '@/components/keys/GroupMonitorHeader.vue'
import { useGroupMonitorStatus } from '@/composables/useGroupMonitorStatus'
import type { GroupPlatform, SubscriptionType } from '@/types'
import PlazaVendorFilter from './PlazaVendorFilter.vue'
import PlazaVendorPriceTable from './PlazaVendorPriceTable.vue'
import { buildVendorTree, collectPlazaModels } from './plazaVendorTree'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { modelVendorLabel } from '@/utils/modelVendor'
import { formatPeakRateWindow, hasPeakRate, serverTimezoneLabel } from '@/utils/peak-rate'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedVendor = ref<string>('all')
const priceViews = ['official', 'site'] as const
type PriceView = typeof priceViews[number]
const priceView = ref<PriceView>('official')
const selectedGroupId = ref<number | null>(null)
const groupPickerOpen = ref(false)
const searchQuery = ref('')
const highlightedKey = ref('')

const searchActive = computed(() => searchQuery.value.trim() !== '')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

const groups = computed(() => props.response?.groups ?? [])

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const sortedGroups = computed(() =>
  [...groups.value].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0) || a.id - b.id)
)

interface PlazaGroupSelectOption extends SelectOption {
  group?: ModelPlazaGroup
}

const groupSelectOptions = computed<PlazaGroupSelectOption[]>(() =>
  sortedGroups.value.map((g) => ({ value: g.id, label: `${g.name} · ${effectiveRate(g)}x`, description: g.description, group: g }))
)

function selectPriceView(view: PriceView) {
  groupPickerOpen.value = false
  if (view === 'site' && !groups.value.some(g => g.id === selectedGroupId.value)) {
    selectedGroupId.value = sortedGroups.value[0]?.id ?? null
  }
  priceView.value = view
}

function onGroupChange(value: string | number | boolean | null) {
  if (typeof value === 'number' && groups.value.some(g => g.id === value)) selectedGroupId.value = value
}

const selectedGroup = computed<ModelPlazaGroup | null>(() =>
  priceView.value === 'site' ? groups.value.find((g) => g.id === selectedGroupId.value) ?? null : null
)

const appStore = useAppStore()
const { statuses: groupMonitorStates } = useGroupMonitorStatus(
  groupPickerOpen,
  groups,
  () => isAuthenticated.value && appStore.cachedPublicSettings?.channel_monitor_enabled !== false &&
    appStore.cachedPublicSettings?.channel_monitor_mode !== 'v2'
)

/** 选中分组的高峰窗口描述(含倍率与服务器时区标注);未启用高峰为空串。 */
const peakWindow = computed(() => {
  const g = selectedGroup.value
  if (!g || g.subscription_type !== 'subscription' || !hasPeakRate(g)) return ''
  return formatPeakRateWindow(g, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const peakNote = computed(() => {
  if (!peakWindow.value || !selectedGroup.value) return ''
  return t('modelPlaza.detail.peakNote', {
    window: peakWindow.value,
    multiplier: selectedGroup.value.peak_rate_multiplier
  })
})

// 数据刷新后选中的分组可能已不可见。
watch(groups, (list) => {
  if (!list.some((g) => g.id === selectedGroupId.value)) {
    selectedGroupId.value = priceView.value === 'site' ? sortedGroups.value[0]?.id ?? null : null
  }
})

const scopeModels = computed(() => collectPlazaModels(groups.value, selectedGroup.value?.id ?? 'all'))

const scopeVendors = computed(() => buildVendorTree(scopeModels.value, '', t('modelPlaza.vendor.other')))

const vendorTree = computed(() =>
  buildVendorTree(scopeModels.value, searchQuery.value, t('modelPlaza.vendor.other'))
)

const tableModels = computed<PlazaModel[]>(() => {
  if (selectedVendor.value === 'all') return vendorTree.value.flatMap((n) => n.models)
  return vendorTree.value.find((n) => n.key === selectedVendor.value)?.models ?? []
})

const currentVendorLabel = computed(() => {
  if (selectedVendor.value === 'all') return t('modelPlaza.vendor.all')
  return vendorTree.value.find((n) => n.key === selectedVendor.value)?.label ?? modelVendorLabel(selectedVendor.value)
})

const emptyMessage = computed(() => {
  if (selectedGroup.value && scopeModels.value.length === 0) return t('modelPlaza.detail.noModels')
  if (selectedVendor.value !== 'all' && !scopeVendors.value.some(vendor => vendor.key === selectedVendor.value)) {
    return selectedGroup.value
      ? t('modelPlaza.detail.noVendorInGroup', { group: selectedGroup.value.name, vendor: currentVendorLabel.value })
      : t('modelPlaza.detail.noVendorInCatalog', { vendor: currentVendorLabel.value })
  }
  return t(searchActive.value ? 'modelPlaza.noSearchResult' : 'modelPlaza.detail.noCatalogModels')
})

function selectVendor(key: string) {
  selectedVendor.value = key
  highlightedKey.value = ''
}

function clearFilters() {
  selectedVendor.value = 'all'
  searchQuery.value = ''
  highlightedKey.value = ''
}
</script>

<style scoped>
.plaza-group-select :deep(.select-trigger) {
  min-height: 2.75rem;
  font-size: 1rem;
  line-height: 1.5rem;
}

.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-sm dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
