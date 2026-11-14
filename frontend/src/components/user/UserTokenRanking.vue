<template>
  <section class="space-y-5" data-testid="user-token-ranking">
    <div>
      <p class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('usage.ranking.title') }}</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('usage.ranking.subtitle') }}</p>
    </div>

    <div class="grid grid-cols-4 gap-1.5 sm:flex sm:flex-wrap sm:gap-2" role="tablist" :aria-label="t('usage.ranking.periodLabel')">
      <button
        v-for="option in periodOptions"
        :key="option.value"
        type="button"
        :data-period="option.value"
        role="tab"
        :aria-selected="period === option.value"
        class="whitespace-nowrap rounded-lg border px-1.5 py-2 text-xs font-medium transition-colors sm:px-3 sm:text-sm"
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
        <div class="grid min-w-0 grid-cols-1 gap-2.5 md:grid-cols-3 md:gap-3">
          <article
            v-for="item in podium"
            :key="item.user_id"
            class="grid min-w-0 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 rounded-2xl border p-3.5 shadow-sm transition-transform hover:-translate-y-0.5 md:grid-cols-[minmax(0,1fr)_auto] md:gap-x-2 md:gap-y-0 md:p-5"
            :class="podiumClasses[item.rank - 1]"
          >
            <span
              class="col-start-1 row-span-2 row-start-1 inline-flex h-10 w-10 items-center justify-center rounded-full text-xl shadow-inner md:row-span-1"
              :class="[medalClasses[item.rank - 1], `rank-${rankNames[item.rank - 1]}`]"
              :data-rank="item.rank"
            >
              {{ medals[item.rank - 1] }}
            </span>
            <span class="col-start-2 row-start-1 text-[11px] font-semibold uppercase tracking-wider opacity-70 md:text-right md:text-xs">
              {{ t('usage.ranking.rank', { rank: item.rank }) }}
            </span>
            <p class="col-start-2 row-start-2 min-w-0 truncate text-sm font-semibold md:col-span-2 md:col-start-1 md:mt-4" :title="item.email">{{ item.email || `User #${item.user_id}` }}</p>
            <div class="col-start-3 row-span-2 row-start-1 min-w-0 text-right md:col-span-2 md:col-start-1 md:row-span-1 md:row-start-3 md:mt-2 md:text-left">
              <p class="whitespace-nowrap text-lg font-bold tabular-nums md:text-2xl">{{ formatTokens(item.total_tokens) }}</p>
              <p class="text-[10px] opacity-60 md:mt-0.5">{{ t('usage.ranking.tokens') }}</p>
            </div>
          </article>
        </div>

        <div v-if="restItems.length" class="min-w-0 overflow-hidden rounded-2xl border border-gray-200 dark:border-dark-700">
          <table class="w-full table-fixed text-left text-sm" data-testid="ranking-table">
            <caption class="sr-only">{{ t('usage.ranking.title') }}</caption>
            <colgroup>
              <col class="w-10 sm:w-16" />
              <col />
              <col class="w-24 sm:w-32" />
            </colgroup>
            <thead class="bg-gray-50 text-xs font-semibold text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th scope="col" class="py-3 pl-3 pr-1 sm:pl-4">{{ t('usage.ranking.rankLabel') }}</th>
                <th scope="col" class="px-2 py-3">{{ t('usage.ranking.user') }}</th>
                <th scope="col" class="py-3 pl-1 pr-3 text-right sm:pr-4">{{ t('usage.ranking.tokens') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in restItems" :key="item.user_id" class="transition-colors hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                <td class="py-3.5 pl-3 pr-1 tabular-nums text-gray-400 sm:pl-4">{{ item.rank }}</td>
                <td class="px-2 py-3.5">
                  <span class="block truncate font-medium text-gray-700 dark:text-gray-200" :title="item.email">{{ item.email || `User #${item.user_id}` }}</span>
                </td>
                <td class="whitespace-nowrap py-3.5 pl-1 pr-3 text-right font-semibold tabular-nums text-gray-900 dark:text-gray-100 sm:pr-4">{{ formatTokens(item.total_tokens) }}</td>
              </tr>
            </tbody>
          </table>
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
  'podium-gold border-amber-200 text-amber-950 dark:border-amber-800/60 dark:text-amber-100',
  'podium-silver border-slate-200 text-slate-900 dark:border-slate-700 dark:text-slate-100',
  'podium-bronze border-orange-200 text-orange-950 dark:border-orange-800/60 dark:text-orange-100',
]

const period = ref<TokenRankingPeriod>('today')
const items = ref<TokenRankingItem[]>([])
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
  } catch {
    if (sequence !== requestSequence) return
    items.value = []
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

<style scoped>
/* Keep podium colors independent of shared gradient custom properties. */
.podium-gold {
  background-color: #fffbeb;
  background-image: linear-gradient(135deg, #fffbeb, #fef9c3);
}

.podium-silver {
  background-color: #f8fafc;
  background-image: linear-gradient(135deg, #f8fafc, #f3f4f6);
}

.podium-bronze {
  background-color: #fff7ed;
  background-image: linear-gradient(135deg, #fff7ed, #fef3c7);
}

.dark .podium-gold {
  background-color: #1c1917;
  background-image: linear-gradient(135deg, rgb(69 26 3 / 40%), rgb(66 32 6 / 30%));
}

.dark .podium-silver {
  background-color: #0f172a;
  background-image: linear-gradient(135deg, rgb(15 23 42 / 60%), rgb(17 24 39 / 40%));
}

.dark .podium-bronze {
  background-color: #1c1917;
  background-image: linear-gradient(135deg, rgb(67 20 7 / 40%), rgb(69 26 3 / 30%));
}
</style>
