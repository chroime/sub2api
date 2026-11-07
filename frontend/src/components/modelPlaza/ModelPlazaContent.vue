<template>
  <div class="model-plaza-content space-y-6">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div v-if="!embedded">
      <h1 class="text-3xl font-bold tracking-tight plaza-heading text-gray-900 dark:text-white sm:text-4xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-2 text-base leading-relaxed plaza-muted text-gray-500 dark:text-dark-400">{{ t('modelPlaza.description') }}</p>
    </div>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-gray-100 bg-white px-5 py-4 text-[15px] shadow-card dark:border-dark-700/50 dark:bg-dark-800/50 sm:text-base"
      v-html="descriptionHtml"
    ></div>

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

    <!-- 左侧厂商树 + 右侧模型价格 -->
    <div v-else class="grid gap-5 lg:grid-cols-[260px_minmax(0,1fr)]">
      <!-- min-w-0：窄屏单列时横向标签栏不能把网格撑出视口 -->
      <aside class="min-w-0 lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] lg:self-start lg:overflow-y-auto">
        <PlazaVendorTree
          :vendors="vendorTree"
          :vendor="selectedVendor"
          :search="searchQuery"
          :highlighted="highlightedKey"
          @update:vendor="selectVendor"
          @update:search="searchQuery = $event"
          @select-model="focusModel"
        />
      </aside>

      <section class="min-w-0 space-y-4">
        <!-- 全部分组只看官方价；选中分组后展示本站实付价与官方价对照。 -->
        <div class="flex flex-wrap items-center gap-3">
          <label for="plaza-group-select" class="text-base font-semibold text-gray-700 dark:text-dark-200">
            {{ t('modelPlaza.priceTable.groupLabel') }}
          </label>
          <div class="plaza-group-select min-w-0 w-full sm:w-80">
            <Select
              id="plaza-group-select"
              :model-value="selectedGroupId"
              :options="groupSelectOptions"
              :search-placeholder="t('modelPlaza.priceTable.groupLabel')"
              @update:model-value="onGroupChange"
            >
              <template #option="{ option, selected }">
                <span class="min-w-0 flex-1 truncate text-left text-[15px] leading-6">{{ option.label }}</span>
                <Icon v-if="selected" name="check" size="sm" class="shrink-0 text-primary-500" :stroke-width="2" />
              </template>
            </Select>
          </div>
          <!-- 选中分组后倍率已在下拉框和表头标出，不再重复 -->
          <span v-if="!selectedGroup" class="text-sm leading-relaxed plaza-soft text-gray-400 dark:text-dark-500">
            {{ t('modelPlaza.priceTable.allGroupsHint') }}
          </span>
        </div>

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

        <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
          <h2 class="shrink-0 text-xl font-semibold text-gray-900 dark:text-white">{{ currentVendorLabel }}</h2>
          <span class="text-sm leading-relaxed plaza-soft text-gray-400 dark:text-dark-500">
            {{ t('modelPlaza.priceTable.unitNote') }}
          </span>
        </div>

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
          {{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.detail.noModels') }}
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import PlazaVendorTree from './PlazaVendorTree.vue'
import PlazaVendorPriceTable from './PlazaVendorPriceTable.vue'
import { buildVendorTree, collectPlazaModels, plazaModelKey } from './plazaVendorTree'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
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
const selectedGroupId = ref<number | 'all'>('all')
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
  [...groups.value].sort((a, b) => effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name))
)

const groupSelectOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('modelPlaza.priceTable.allGroups') },
  ...sortedGroups.value.map((g) => ({ value: g.id, label: `${g.name} · ${effectiveRate(g)}x` }))
])

function onGroupChange(value: string | number | boolean | null) {
  selectedGroupId.value = typeof value === 'number' ? value : 'all'
}

const selectedGroup = computed<ModelPlazaGroup | null>(() =>
  selectedGroupId.value === 'all' ? null : groups.value.find((g) => g.id === selectedGroupId.value) ?? null
)

const appStore = useAppStore()

/** 选中分组的高峰窗口描述(含倍率与服务器时区标注);未启用高峰为空串。 */
const peakWindow = computed(() => {
  const g = selectedGroup.value
  if (!g || !hasPeakRate(g)) return ''
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
  if (selectedGroupId.value !== 'all' && !list.some((g) => g.id === selectedGroupId.value)) {
    selectedGroupId.value = 'all'
  }
})

const vendorTree = computed(() =>
  buildVendorTree(collectPlazaModels(groups.value, selectedGroupId.value), searchQuery.value, t('modelPlaza.vendor.other'))
)

// 切换分组或搜索后选中的厂商可能没有模型了。
watch(vendorTree, (nodes) => {
  if (selectedVendor.value !== 'all' && !nodes.some((n) => n.key === selectedVendor.value)) {
    selectedVendor.value = 'all'
  }
})

const tableModels = computed<PlazaModel[]>(() => {
  if (selectedVendor.value === 'all') return vendorTree.value.flatMap((n) => n.models)
  return vendorTree.value.find((n) => n.key === selectedVendor.value)?.models ?? []
})

const currentVendorLabel = computed(() => {
  if (selectedVendor.value === 'all') return t('modelPlaza.vendor.all')
  return vendorTree.value.find((n) => n.key === selectedVendor.value)?.label ?? ''
})

function selectVendor(key: string) {
  selectedVendor.value = key
  highlightedKey.value = ''
}

/** 点击树中模型:切到其厂商并滚动到对应行。 */
async function focusModel(model: PlazaModel, vendor: string) {
  selectedVendor.value = vendor
  highlightedKey.value = plazaModelKey(model)
  await nextTick()
  const id = `plaza-model-${highlightedKey.value.replace(/[^a-zA-Z0-9_-]/g, '_')}`
  document.getElementById(id)?.scrollIntoView({ block: 'center', behavior: 'smooth' })
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
