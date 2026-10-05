<template>
  <AppLayout>
    <div class="iq-page mx-auto min-h-full max-w-7xl space-y-8 px-4 py-6 sm:px-6 lg:px-8" data-test="iq-detection-page">
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div>
        <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-primary-600 dark:text-primary-300">
          <Icon name="brain" size="sm" />
          <span>{{ t('iqDetection.eyebrow') }}</span>
        </div>
        <h1 class="mt-2 text-2xl font-semibold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('iqDetection.title') }}</h1>
        <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-500 dark:text-dark-300">{{ t('iqDetection.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="loading" data-test="iq-refresh" @click="load">
        <Icon name="refresh" size="sm" />
        {{ loading ? t('common.loading') : t('common.refresh') }}
      </button>
    </header>

    <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-200">{{ error }}</p>
    <p v-if="loading" role="status" class="rounded-xl border border-gray-200 bg-white px-5 py-10 text-center text-sm text-gray-500 shadow-sm dark:border-dark-700 dark:bg-dark-800">{{ t('common.loading') }}</p>

    <template v-else>
      <section class="space-y-3" aria-labelledby="iq-candy-title">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="iq-candy-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('iqDetection.candyResults') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.candyHint', { answer: dashboard.standard_answer }) }}</p>
          </div>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.recentCount', { count: dashboard.candy_results.length }) }}</span>
        </div>
        <div v-if="dashboard.candy_results.length" class="grid gap-px overflow-hidden rounded-2xl border border-gray-200 bg-gray-200 shadow-sm dark:border-dark-700 dark:bg-dark-700 sm:grid-cols-2 xl:grid-cols-4">
          <article v-for="result in dashboard.candy_results" :key="result.id" class="bg-white p-4 dark:bg-dark-800" data-test="candy-result-card">
            <div class="flex items-start justify-between gap-3">
              <p class="min-w-0 truncate text-sm font-medium text-gray-800 dark:text-dark-100" :title="result.group_name || result.account_label">{{ result.group_name || result.account_label }}</p>
              <span class="shrink-0 text-xs text-gray-400">{{ formatTime(result.created_at) }}</span>
            </div>
            <div class="mt-4 flex items-end justify-between gap-4">
              <div>
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.modelAnswer') }}</p>
                <p class="mt-1 text-2xl font-semibold text-emerald-700 dark:text-emerald-300">{{ result.answer ?? '—' }}<span class="ml-1 text-xs font-normal text-gray-500">{{ t('iqDetection.items') }}</span></p>
              </div>
              <Icon :name="isCandyPassing(result) ? 'checkCircle' : 'exclamationCircle'" size="lg" :class="isCandyPassing(result) ? 'text-emerald-600' : 'text-amber-500'" />
            </div>
            <div class="mt-4 flex items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-400">
              <span class="truncate">{{ result.model }} · {{ effortLabel(result.effort) }}</span>
              <span class="rounded-full px-2 py-1" :class="isCandyPassing(result) ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'">{{ verdictLabel(result) }}</span>
            </div>
          </article>
        </div>
        <div v-else class="rounded-xl border border-dashed border-gray-300 bg-white px-5 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-800">{{ t('iqDetection.emptyCandy') }}</div>
      </section>

      <section class="space-y-4 rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800" aria-labelledby="iq-timeline-title">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="iq-timeline-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('iqDetection.timelineTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.timelineHint') }}</p>
          </div>
          <div class="flex flex-wrap gap-3 text-xs text-gray-500 dark:text-dark-400"><span><i class="iq-dot bg-emerald-500" />{{ t('iqDetection.pass') }}</span><span><i class="iq-dot bg-rose-500" />{{ t('iqDetection.fail') }}</span><span><i class="iq-dot bg-amber-400" />{{ t('iqDetection.empty') }}</span></div>
        </div>
        <div class="overflow-x-auto" data-test="iq-timeline" :aria-label="t('iqDetection.timelineTitle')">
          <div class="flex min-w-[388px] gap-1">
            <span v-for="point in dashboard.timeline" :key="point.at" class="h-8 min-w-1 flex-1 rounded-sm" :class="timelineClass(point.status)" :title="`${formatTime(point.at)} · ${timelineLabel(point.status)}`" />
          </div>
        </div>
        <div class="flex justify-between text-[11px] text-gray-400"><span>{{ t('iqDetection.hoursAgo', { hours: dashboard.window_hours }) }}</span><span>{{ t('iqDetection.now') }}</span></div>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.timelineDisclaimer') }}</p>
      </section>

      <section class="space-y-4" aria-labelledby="iq-pelican-title">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="iq-pelican-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('iqDetection.pelicanTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.pelicanHint') }}</p>
          </div>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('iqDetection.recentCount', { count: dashboard.pelican_works.length }) }}</span>
        </div>
        <div v-if="dashboard.pelican_works.length" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <article v-for="work in dashboard.pelican_works" :key="work.id" class="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800" data-test="pelican-work-card">
            <div class="relative aspect-[4/3] overflow-hidden bg-slate-50 dark:bg-dark-900">
              <iframe :key="`${work.id}:${replayNonce[work.id] || 0}`" :srcdoc="preview(work.html)" :title="`${t('iqDetection.pelicanTitle')} ${work.model}`" sandbox="allow-scripts" referrerpolicy="no-referrer" class="h-full w-full border-0" />
              <button type="button" class="absolute bottom-3 right-3 inline-flex items-center gap-1.5 rounded-full bg-slate-900/85 px-3 py-1.5 text-xs font-medium text-white shadow-lg transition hover:bg-slate-900" @click="replay(work.id)"><Icon name="play" size="xs" />{{ t('iqDetection.replay') }}</button>
            </div>
            <div class="space-y-2 p-4 text-sm">
              <div class="flex justify-between gap-3"><span class="truncate font-medium text-gray-800 dark:text-dark-100">{{ work.group_name || work.account_label }}</span><time class="shrink-0 text-xs text-gray-400">{{ formatTime(work.created_at) }}</time></div>
              <div class="flex justify-between gap-3 text-xs text-gray-500 dark:text-dark-400"><span class="truncate">{{ work.model }}</span><span>{{ effortLabel(work.effort) }}</span></div>
              <div class="flex justify-between gap-3 text-xs text-gray-500 dark:text-dark-400"><span>{{ t('iqDetection.generated') }}</span><span>{{ work.duration_ms }} ms</span></div>
            </div>
          </article>
        </div>
        <div v-else class="rounded-xl border border-dashed border-gray-300 bg-white px-5 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-800">{{ t('iqDetection.emptyPelican') }}</div>
      </section>
    </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import userAPI, { type IQDashboard, type IQCandyResult } from '@/api/user'
import { modelPreviewDocument, modelPreviewCSP } from '@/views/admin/upstream-governance/model-preview'

const { t } = useI18n()
const loading = ref(true)
const error = ref('')
const dashboard = ref<IQDashboard>({ candy_results: [], pelican_works: [], timeline: [], standard_answer: 21, window_hours: 24, generated_at: '' })
const replayNonce = ref<Record<string, number>>({})

async function load() {
  loading.value = true; error.value = ''
  try { dashboard.value = await userAPI.getIQDetection({ hours: 24, limit: 8 }) } catch { error.value = t('iqDetection.loadFailed') } finally { loading.value = false }
}
function formatTime(value: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '—'
  const parts = new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23', timeZone: 'Asia/Shanghai' }).formatToParts(date)
  const values = Object.fromEntries(parts.map(({ type, value: part }) => [type, part]))
  return `${values.year}-${values.month}-${values.day} ${values.hour}:${values.minute}:${values.second}`
}
function effortLabel(value: string) { return value?.trim() || '—' }
function isCandyPassing(value: IQCandyResult) { return value.status === 'succeeded' && (value.verdict === 'numeric_correct' || value.verdict === 'pass') }
function verdictLabel(value: IQCandyResult) { return isCandyPassing(value) ? t('iqDetection.rulePass') : t('iqDetection.needsReview') }
function timelineClass(status: string) { return status === 'pass' ? 'bg-emerald-500' : status === 'fail' ? 'bg-rose-500' : 'bg-amber-300' }
function timelineLabel(status: string) { return status === 'pass' ? t('iqDetection.pass') : status === 'fail' ? t('iqDetection.fail') : t('iqDetection.empty') }
function preview(html: string) { return `${modelPreviewDocument(html)}<!-- ${modelPreviewCSP} -->` }
function replay(id: string) { replayNonce.value[id] = (replayNonce.value[id] || 0) + 1 }
onMounted(load)
</script>

<style scoped>
.iq-page { background: linear-gradient(180deg, rgba(239, 246, 243, .7), transparent 18rem); }
.iq-dot { display: inline-block; width: .55rem; height: .55rem; border-radius: 9999px; margin-right: .35rem; vertical-align: .02rem; }
</style>
