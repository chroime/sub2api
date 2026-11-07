<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled } from '@/utils/pricing'
import { comparePriceAmounts, type PlazaPriceComparison } from './plazaPriceComparison'

const props = defineProps<{
  price: PlazaPriceComparison
  unit?: string
}>()

const { t } = useI18n()
const comparison = computed(() => props.price.comparable
  ? comparePriceAmounts(props.price.reference, props.price.site)
  : { status: 'unavailable', difference: null, percent: null })
const referenceLabel = computed(() => t(`modelPlaza.comparison.${props.price.referenceKind === 'configured' ? 'configuredReference' : 'officialReference'}`))

function money(value: number | null): string {
  if (value == null || !Number.isFinite(value)) return '—'
  const digits = value === 0 ? 1 : Math.floor(Math.log10(Math.abs(value))) + 1
  const rounded = Number(value.toPrecision(Math.min(100, Math.max(5, digits + 2))))
  const formatted = formatScaled(rounded, 1, 2)
  return props.price.currency === 'CNY' ? formatted.replace(/^\$/, '¥') : formatted
}

const percent = computed(() => {
  const value = comparison.value.percent
  if (value == null) return ''
  const magnitude = Math.abs(value)
  if (magnitude > 0 && magnitude < 0.01) return '<0.01%'
  // Do not turn a positive payable price into an apparent 100% discount.
  const rounded = Math.floor(Number(magnitude.toPrecision(12)) * 100 + 1e-9) / 100
  return `${Math.min(rounded, comparison.value.status === 'saving' && props.price.site !== 0 ? 99.99 : rounded)}%`
})

const differenceLabel = computed(() => {
  const result = comparison.value
  if (result.status === 'equal') return t('modelPlaza.comparison.samePrice')
  if (result.difference == null) return ''
  return t(result.status === 'saving' ? 'modelPlaza.comparison.saveAmount' : 'modelPlaza.comparison.extraAmount', {
    amount: money(Math.abs(result.difference))
  })
})
</script>

<template>
  <div class="plaza-price-pair relative" :data-comparison="comparison.status">
    <div v-if="price.label" class="mb-1.5 text-xs font-medium text-gray-600 dark:text-dark-300" data-price-tier>
      {{ price.label }}
    </div>
    <template v-if="price.reference != null || price.site != null">
      <div class="flex flex-wrap items-baseline justify-end gap-x-1.5 text-xs leading-5 text-gray-600 dark:text-dark-300">
        <span>{{ referenceLabel }}</span>
        <span class="font-mono tabular-nums" :class="{ 'line-through decoration-gray-400/70': comparison.status === 'saving' }" data-reference-price>
          {{ money(price.reference) }}<span v-if="unit && price.reference != null" class="font-sans">{{ unit }}</span>
        </span>
      </div>
      <div class="my-1.5 flex flex-wrap items-baseline justify-end gap-x-1">
        <span class="sr-only">{{ t('modelPlaza.priceTable.sitePrice') }}</span>
        <span class="plaza-payable font-mono text-2xl font-bold leading-8 tracking-tight tabular-nums" data-site-price>
          {{ money(price.site) }}<span v-if="unit && price.site != null" class="ml-1 font-sans text-xs font-medium tracking-normal">{{ unit }}</span>
        </span>
      </div>
      <div v-if="comparison.status !== 'unavailable'" class="flex justify-end">
        <span
          class="inline-flex flex-wrap items-center justify-end gap-x-2 gap-y-1.5 rounded-lg px-2 py-1.5 text-[13px] font-semibold leading-5"
          :class="comparison.status === 'saving' ? 'plaza-saving' : comparison.status === 'higher' ? 'bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300' : 'text-gray-500 dark:text-dark-300'"
          :title="t(price.referenceKind === 'configured' ? 'modelPlaza.comparison.configuredBasis' : 'modelPlaza.comparison.officialBasis')"
          data-price-difference
        >
          <span v-if="percent && comparison.status !== 'equal'" class="rounded-md px-2 py-0.5 text-base font-bold tabular-nums" :class="comparison.status === 'saving' ? 'plaza-discount' : 'bg-amber-100 dark:bg-amber-900/50'">
            {{ t(comparison.status === 'saving' ? 'modelPlaza.comparison.savePercent' : 'modelPlaza.comparison.extraPercent', { percent }) }}
          </span>
          <span>{{ differenceLabel }}</span>
        </span>
      </div>
      <span v-else-if="price.reference == null" class="text-xs text-gray-500 dark:text-dark-300">{{ t('modelPlaza.comparison.noReference') }}</span>
      <span v-else-if="price.site == null" class="text-xs text-gray-500 dark:text-dark-300">{{ t('modelPlaza.comparison.noSitePrice') }}</span>
      <span v-else class="text-xs text-gray-500 dark:text-dark-300">{{ t('modelPlaza.comparison.differentBasis') }}</span>
    </template>
    <span v-else class="text-sm text-gray-400 dark:text-dark-400">—</span>
  </div>
</template>

<style scoped>
.plaza-payable { color: var(--public-accent, theme('colors.emerald.700')); }
.plaza-saving { color: var(--public-accent, theme('colors.emerald.800')); background: var(--public-accent-soft, theme('colors.emerald.50')); }
.plaza-discount { color: var(--public-accent-ink, theme('colors.white')); background: var(--public-accent, theme('colors.emerald.700')); }
.dark .plaza-payable { color: var(--public-accent, theme('colors.emerald.300')); }
.dark .plaza-saving { color: var(--public-accent, theme('colors.emerald.300')); background: var(--public-accent-soft, theme('colors.emerald.950 / 40%')); }
.dark .plaza-discount { color: var(--public-accent-ink, theme('colors.emerald.950')); background: var(--public-accent, theme('colors.emerald.300')); }
</style>
