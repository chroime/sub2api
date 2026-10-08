<template>
  <section class="space-y-5" data-testid="user-token-ranking">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <p class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('usage.ranking.title') }}</p>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('usage.ranking.subtitle') }}</p>
      </div>
      <div class="rounded-xl border border-primary-100 bg-primary-50 px-4 py-3 text-left dark:border-primary-900/40 dark:bg-primary-950/30 sm:text-right">
        <p class="text-xs font-medium uppercase tracking-wide text-primary-600 dark:text-primary-300">{{ t('usage.ranking.total') }}</p>
        <p class="mt-1 text-xl font-bold tabular-nums text-primary-700 dark:text-primary-200">{{ formatTokens(totalTokens) }}</p>
      </div>
    </div>

    <div class="flex flex-wrap gap-2" role="tablist" :aria-label="t('usage.ranking.periodLabel')">
      <button
        v-for="option in periodOptions"
        :key="option.value"
        type="button"
        :data-period="option.value"
        role="tab"
        :aria-selected="period === option.value"
        class="rounded-lg border px-3 py-2 text-sm font-medium transition-colors"
        :class="period === option.value
          ? 'border-primary-500 bg-primary-500 text-white shadow-sm'
          : 'border-gray-200 bg-white text-gray-600 hover:border-primary-300 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:border-primary-700 dark:hover:text-primary-300'"
        @click="selectPeriod(option.value)"
      >
        {{ t(option.label) }}
      </button>
    </div>

    <p v-if="error" class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300">
      {{ t('usage.ranking.loadFailed') }}
    </p>

    <div v-if="loading" class="flex min-h-48 items-center justify-center rounded-2xl border border-gray-100 bg-gray-50/70 dark:border-dark-700 dark:bg-dark-900/50">
      <LoadingSpinner />
    </div>

    <template v-else>
      <p v-if="items.length === 0" class="rounded-2xl border border-dashed border-gray-200 px-4 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        {{ t('usage.ranking.empty') }}
      </p>

      <div v-else class="space-y-4">
        <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
          <article
            v-for="item in podium"
            :key="item.user_id"
            class="rounded-2xl border p-4 shadow-sm transition-transform hover:-translate-y-0.5"
            :class="podiumClasses[item.rank - 1]"
          >
            <div class="flex items-center justify-between gap-3">
              <span
                class="inline-flex h-10 w-10 items-center justify-center rounded-full text-xl shadow-inner"
                :class="[medalClasses[item.rank - 1], `rank-${rankNames[item.rank - 1]}`]"
                :data-rank="item.rank"
              >
                {{ medals[item.rank - 1] }}
              </span>
              <span class="text-xs font-semibold uppercase tracking-wider opacity-70">
                {{ t('usage.ranking.rank', { rank: item.rank }) }}
              </span>
            </div>
            <p class="mt-4 truncate text-sm font-semibold" :title="item.email">{{ item.email || `User #${item.user_id}` }}</p>
            <p class="mt-2 text-2xl font-bold tabular-nums">{{ formatTokens(item.total_tokens) }}</p>
            <p class="mt-1 text-xs opacity-70">{{ t('usage.ranking.requests', { count: item.requests.toLocaleString() }) }}</p>
          </article>
        </div>

        <div class="overflow-hidden rounded-2xl border border-gray-200 dark:border-dark-700">
          <div class="grid grid-cols-[3rem_minmax(0,1fr)_7rem_7rem] gap-3 bg-gray-50 px-4 py-3 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400 sm:grid-cols-[4rem_minmax(0,1fr)_8rem_8rem]">
            <span>{{ t('usage.ranking.rankLabel') }}</span>
            <span>{{ t('usage.ranking.user') }}</span>
            <span class="text-right">{{ t('usage.ranking.requestsLabel') }}</span>
            <span class="text-right">{{ t('usage.ranking.total') }}</span>
          </div>
          <div class="divide-y divide-gray-100 dark:divide-dark-700">
            <div
              v-for="item in restItems"
              :key="item.user_id"
              class="grid grid-cols-[3rem_minmax(0,1fr)_7rem_7rem] items-center gap-3 px-4 py-3 text-sm sm:grid-cols-[4rem_minmax(0,1fr)_8rem_8rem]"
            >
              <span class="tabular-nums text-gray-400">{{ item.rank }}</span>
              <span class="truncate font-medium text-gray-700 dark:text-gray-200" :title="item.email">{{ item.email || `User #${item.user_id}` }}</span>
              <span class="text-right tabular-nums text-gray-500 dark:text-gray-400">{{ item.requests.toLocaleString() }}</span>
              <span class="text-right font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatTokens(item.total_tokens) }}</span>
            </div>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { usageAPI } from '@/api'
import type { TokenRankingItem, TokenRankingPeriod } from '@/api/usage'
import { formatCompactNumber } from '@/utils/format'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const { t } = useI18n()

const periodOptions: Array<{ value: TokenRankingPeriod; label: string }> = [
  { value: 'today', label: 'usage.ranking.periods.today' },
  { value: 'yesterday', label: 'usage.ranking.periods.yesterday' },
  { value: '7d', label: 'usage.ranking.periods.7d' },
  { value: '30d', label: 'usage.ranking.periods.30d' },
]

const medals = ['🥇', '🥈', '🥉']
const rankNames = ['gold', 'silver', 'bronze']
const medalClasses = [
  'bg-amber-100 dark:bg-amber-500/20',
  'bg-slate-200 dark:bg-slate-500/20',
  'bg-orange-100 dark:bg-orange-500/20',
]
const podiumClasses = [
  'border-amber-200 bg-gradient-to-br from-amber-50 to-yellow-100 text-amber-950 dark:border-amber-800/60 dark:from-amber-950/40 dark:to-yellow-950/30 dark:text-amber-100',
  'border-slate-200 bg-gradient-to-br from-slate-50 to-gray-100 text-slate-900 dark:border-slate-700 dark:from-slate-900/60 dark:to-gray-900/40 dark:text-slate-100',
  'border-orange-200 bg-gradient-to-br from-orange-50 to-amber-100 text-orange-950 dark:border-orange-800/60 dark:from-orange-950/40 dark:to-amber-950/30 dark:text-orange-100',
]

const period = ref<TokenRankingPeriod>('today')
const items = ref<TokenRankingItem[]>([])
const totalTokens = ref(0)
const loading = ref(false)
const error = ref(false)
let requestSequence = 0

const podium = computed(() => items.value.slice(0, 3))
const restItems = computed(() => items.value.slice(3))
const formatTokens = (value: number) => formatCompactNumber(value)

const load = async () => {
  const sequence = ++requestSequence
  loading.value = true
  error.value = false
  try {
    const response = await usageAPI.getTokenRanking(period.value)
    if (sequence !== requestSequence) return
    items.value = response.ranking.map((item, index) => ({ ...item, rank: item.rank || index + 1 }))
    totalTokens.value = response.total_tokens
  } catch {
    if (sequence !== requestSequence) return
    items.value = []
    totalTokens.value = 0
    error.value = true
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

const selectPeriod = (next: TokenRankingPeriod) => {
  if (period.value === next) return
  period.value = next
  void load()
}

onMounted(() => {
  void load()
})

defineExpose({ load })
</script>
