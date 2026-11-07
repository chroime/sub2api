<template>
  <nav
    class="plaza-vendor-tree rounded-2xl border border-gray-100 bg-white p-3 shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
    :aria-label="t('modelPlaza.vendor.treeLabel')"
  >
    <div class="relative mb-3.5">
      <Icon
        name="search"
        size="sm"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 plaza-soft text-gray-400 dark:text-dark-500"
      />
      <input
        :value="search"
        type="search"
        class="plaza-search input rounded-lg py-2.5 pl-9 text-[15px] leading-5"
        :placeholder="t('modelPlaza.filters.searchPlaceholder')"
        :aria-label="t('modelPlaza.filters.searchPlaceholder')"
        @input="emit('update:search', ($event.target as HTMLInputElement).value)"
      />
    </div>

    <!-- 窄屏：厂商平铺成可换行的标签，全部一眼可见，也不会像竖排列表那样把价格表挤到下面 -->
    <div class="flex flex-wrap gap-2 lg:hidden" data-vendor-chips>
      <button
        v-for="chip in chips"
        :key="chip.key"
        type="button"
        class="flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg px-3 py-2 text-[15px] font-medium transition-colors"
        :class="rowClass(vendor === chip.key)"
        :aria-pressed="vendor === chip.key"
        @click="emit('update:vendor', chip.key)"
      >
        <ModelIcon v-if="chip.sampleModel" :model="chip.sampleModel" size="16px" />
        <Icon v-else name="grid" size="sm" class="h-4 w-4" />
        {{ chip.label }}
        <span class="text-sm tabular-nums opacity-70">{{ chip.count }}</span>
      </button>
    </div>

    <ul class="hidden space-y-0.5 lg:block" role="tree">
      <li role="treeitem" :aria-selected="vendor === 'all'">
        <button
          type="button"
          class="flex w-full items-center gap-2 rounded-lg px-2.5 py-2.5 text-[15px] font-medium transition-colors"
          :class="rowClass(vendor === 'all')"
          @click="emit('update:vendor', 'all')"
        >
          <Icon name="grid" size="sm" class="h-4 w-4 shrink-0" />
          <span class="flex-1 truncate text-left">{{ t('modelPlaza.vendor.all') }}</span>
          <span class="text-sm tabular-nums opacity-70">{{ total }}</span>
        </button>
      </li>

      <li
        v-for="node in vendors"
        :key="node.key"
        role="treeitem"
        :aria-expanded="isExpanded(node.key)"
        :aria-selected="vendor === node.key"
      >
        <div class="flex items-center gap-0.5">
          <button
            type="button"
            class="flex h-8 w-7 shrink-0 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200"
            :aria-label="t(isExpanded(node.key) ? 'modelPlaza.vendor.collapse' : 'modelPlaza.vendor.expand', { vendor: node.label })"
            @click="toggle(node.key)"
          >
            <Icon :name="isExpanded(node.key) ? 'chevronDown' : 'chevronRight'" size="xs" class="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            class="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2 py-2.5 text-[15px] font-medium transition-colors"
            :class="rowClass(vendor === node.key)"
            @click="selectVendor(node.key)"
          >
            <ModelIcon :model="node.sampleModel" size="16px" />
            <span class="flex-1 truncate text-left">{{ node.label }}</span>
            <span class="text-sm tabular-nums opacity-70">{{ node.models.length }}</span>
          </button>
        </div>
        <ul
          v-if="isExpanded(node.key)"
          role="group"
          class="mb-1 ml-[1.1rem] mt-0.5 space-y-px border-l border-gray-100 pl-2 dark:border-dark-700/60"
        >
          <li v-for="m in node.models" :key="plazaModelKey(m)" role="treeitem" :aria-selected="highlighted === plazaModelKey(m)">
            <button
              type="button"
              class="block w-full truncate rounded-md px-2 py-1.5 text-left font-mono text-sm leading-6 transition-colors"
              :class="highlighted === plazaModelKey(m)
                ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300'
                : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-700/60 dark:hover:text-white'"
              :title="m.name"
              @click="emit('select-model', m, node.key)"
            >
              {{ m.name }}
            </button>
          </li>
        </ul>
      </li>
    </ul>

    <p v-if="vendors.length === 0" class="px-2 py-6 text-center text-sm plaza-soft text-gray-400 dark:text-dark-500">
      {{ t('modelPlaza.noSearchResult') }}
    </p>
  </nav>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import type { PlazaModel } from '@/api/modelPlaza'
import { plazaModelKey, type PlazaVendorNode } from './plazaVendorTree'

const props = defineProps<{
  vendors: PlazaVendorNode[]
  /** 当前选中的厂商 key，'all' 为全部。 */
  vendor: string
  search: string
  /** 当前高亮的模型（plazaModelKey）。 */
  highlighted?: string
}>()

const emit = defineEmits<{
  'update:vendor': [value: string]
  'update:search': [value: string]
  'select-model': [model: PlazaModel, vendor: string]
}>()

const { t } = useI18n()

const total = computed(() => props.vendors.reduce((sum, n) => sum + n.models.length, 0))

const chips = computed(() => [
  { key: 'all', label: t('modelPlaza.vendor.all'), count: total.value, sampleModel: '' },
  ...props.vendors.map((n) => ({ key: n.key, label: n.label, count: n.models.length, sampleModel: n.sampleModel }))
])

const expanded = ref(new Set<string>())

/** 搜索时展开全部厂商，便于直接看到命中的模型。 */
function isExpanded(key: string): boolean {
  return props.search.trim() !== '' || expanded.value.has(key)
}

function toggle(key: string) {
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}

function selectVendor(key: string) {
  emit('update:vendor', key)
  if (!expanded.value.has(key)) toggle(key)
}

// 外部切到某个厂商（如点击右侧模型）时同步展开。
watch(
  () => props.vendor,
  (key) => {
    if (key !== 'all' && !expanded.value.has(key)) toggle(key)
  }
)

function rowClass(active: boolean): string {
  return active
    ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300'
    : 'text-gray-700 hover:bg-gray-50 hover:text-gray-900 dark:text-dark-200 dark:hover:bg-dark-700/60 dark:hover:text-white'
}
</script>
