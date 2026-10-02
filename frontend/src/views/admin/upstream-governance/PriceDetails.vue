<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { RemotePrice } from '@/api/admin/upstream-governance'
defineProps<{ prices?: RemotePrice[] }>()
const { t } = useI18n()
const units: Record<string, string> = {
  usd_per_token: 'usdPerMillionTokens',
  usd_per_request: 'usdPerRequest',
  newapi_ratio: 'newapiRatio',
}
const amount = (value: number | null | undefined, unit = '') =>
  value == null
    ? t('governance.unknown')
    : unit === 'usd_per_token'
      ? Number((value * 1_000_000).toPrecision(12))
      : value
</script>
<template>
  <details class="text-xs">
    <summary class="cursor-pointer text-primary-700 dark:text-primary-300">
      {{ t('governance.prices') }}
      <span class="text-gray-400">{{ prices?.length || '—' }}</span>
    </summary>
    <p v-if="!prices?.length" class="mt-2 text-gray-500">
      {{ t('governance.unknown') }}
    </p>
    <template v-else
      ><p class="my-2 text-gray-500">
        {{ t('governance.priceMultiplierHint') }}
      </p>
      <div
        v-for="(price, index) in prices"
        :key="index"
        class="my-2 rounded-lg bg-gray-50 p-2 dark:bg-dark-900"
      >
        <strong>{{ price.model }}</strong> ·
        {{ price.platform || t('governance.unknown') }} ·
        {{
          units[price.unit]
            ? t('governance.' + units[price.unit])
            : price.unit || t('governance.unknown')
        }}
        <p class="mt-1">
          {{ t('governance.input') }}
          <span data-test="price-input">{{
            amount(price.input, price.unit)
          }}</span>
          / {{ t('governance.output') }}
          <span data-test="price-output">{{
            amount(price.output, price.unit)
          }}</span>
          / {{ t('governance.request') }} {{ amount(price.per_request) }}
        </p>
        <p v-if="price.details" class="mt-1 break-all text-gray-500">
          {{ t('governance.priceDetails') }}:
          {{ JSON.stringify(price.details) }}
        </p>
      </div></template
    >
  </details>
</template>
