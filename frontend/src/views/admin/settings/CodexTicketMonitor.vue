<template>
  <section id="codex-ticket-monitor" class="scroll-mt-40 space-y-5 p-6" aria-labelledby="codex-ticket-monitor-title" :aria-busy="loading">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 id="codex-ticket-monitor-title" class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.codexTickets.monitor.title') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.monitor.instance') }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.codexTickets.monitor.pollHint') }}</p>
      </div>
      <button id="codex-ticket-monitor-refresh" type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">{{ t(loading ? 'common.loading' : 'admin.settings.codexTickets.monitor.refresh') }}</button>
    </div>
    <div class="grid gap-3 sm:grid-cols-2">
      <div>
        <label for="codex-ticket-monitor-mode" class="input-label">{{ t('admin.settings.codexTickets.monitor.mode') }}</label>
        <select id="codex-ticket-monitor-mode" v-model="mode" class="input">
          <option value="">{{ t('admin.settings.codexTickets.monitor.allModes') }}</option>
          <option value="292">Codex 292</option>
          <option value="332">Codex 332</option>
        </select>
      </div>
      <div>
        <label for="codex-ticket-monitor-account" class="input-label">{{ t('admin.settings.codexTickets.monitor.accountFilter') }}</label>
        <!-- Keep this transient filter out of the parent settings form and credential autofill. -->
        <input
          id="codex-ticket-monitor-account"
          v-model="accountSearch"
          name="codex-ticket-monitor-query"
          type="search"
          form=""
          autocomplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-bwignore="true"
          class="input"
          :placeholder="t('admin.settings.codexTickets.monitor.accountPlaceholder')"
        />
      </div>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.settings.codexTickets.monitor.loadFailed') }}</p>
    <p role="status" class="text-xs text-gray-500 dark:text-gray-400">{{ snapshot ? t('admin.settings.codexTickets.monitor.updated', { time: formatTime(snapshot.updated_at) }) : t(loading ? 'common.loading' : 'admin.settings.codexTickets.monitor.notLoaded') }}</p>

    <div>
      <h4 class="mb-3 text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.settings.codexTickets.monitor.states') }}</h4>
      <div v-if="filteredStates.length" class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.account') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.status') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.route') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.expiry') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.nextAttempt') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.usage') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="state in statePageItems" :key="`${state.mode}:${state.account_id}:${state.model}`" data-testid="ticket-monitor-state" class="align-top text-gray-700 dark:text-gray-300">
              <td class="px-3 py-3">
                <div class="max-w-48 break-words font-medium">{{ state.account_name || `#${state.account_id}` }} <span class="text-xs text-gray-500">#{{ state.account_id }}</span></div>
                <div class="mt-1 whitespace-nowrap text-xs text-gray-500">{{ modeLabel(state.mode) }} · {{ state.model }}</div>
              </td>
              <td class="px-3 py-3">
                <span :class="['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', statusClass(state.status)]">{{ enumLabel('statuses', state.status) }}</span>
                <p class="mt-1 text-xs text-gray-500">{{ enumLabel('phases', state.phase) }}</p>
                <p v-if="state.last_error" class="mt-1 max-w-64 text-xs text-amber-700 dark:text-amber-400">{{ enumLabel('errors', state.last_error) }}</p>
              </td>
              <td class="max-w-40 break-words px-3 py-3">{{ state.proxy_name || '—' }}<span v-if="state.proxy_id" class="block text-xs text-gray-500">#{{ state.proxy_id }}</span></td>
              <td class="whitespace-nowrap px-3 py-3 text-xs">{{ formatTime(state.expires_at) }}<span v-if="state.length" class="mt-1 block text-gray-500">{{ state.length }} B</span></td>
              <td class="whitespace-nowrap px-3 py-3 text-xs">{{ formatTime(state.next_attempt_at) }}</td>
              <td class="whitespace-nowrap px-3 py-3 text-xs">{{ t('admin.settings.codexTickets.monitor.uses', { count: state.uses }) }}<span class="mt-1 block text-gray-500">{{ formatTime(state.last_used_at) }}</span></td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-800 dark:text-gray-400">{{ t('admin.settings.codexTickets.monitor.emptyStates') }}</p>
      <div v-if="filteredStates.length" class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500">
        <span>{{ t('admin.settings.codexTickets.monitor.page', { page: statePage, pages: statePages, total: filteredStates.length }) }}</span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="statePage <= 1" @click="statePage--">{{ t('admin.settings.codexTickets.monitor.previous') }}</button>
          <button id="codex-ticket-monitor-states-next" type="button" class="btn btn-secondary btn-sm" :disabled="statePage >= statePages" @click="statePage++">{{ t('admin.settings.codexTickets.monitor.next') }}</button>
        </div>
      </div>
    </div>

    <div>
      <h4 class="mb-3 text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.settings.codexTickets.monitor.events') }}</h4>
      <div v-if="filteredEvents.length" class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.time') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.account') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.outcome') }}</th>
              <th scope="col" class="px-3 py-2">{{ t('admin.settings.codexTickets.monitor.route') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="event in eventPageItems" :key="event.id" data-testid="ticket-monitor-event" class="align-top text-gray-700 dark:text-gray-300">
              <td class="whitespace-nowrap px-3 py-3 text-xs">{{ formatTime(event.time) }}</td>
              <td class="px-3 py-3"><div class="max-w-48 break-words">{{ event.account_name || `#${event.account_id}` }} <span class="text-xs text-gray-500">#{{ event.account_id }}</span></div><div class="mt-1 whitespace-nowrap text-xs text-gray-500">{{ modeLabel(event.mode) }} · {{ event.model }}</div></td>
              <td class="px-3 py-3">
                <div class="text-xs">{{ enumLabel('phases', event.phase) }} · {{ enumLabel('statuses', event.status) }}</div>
                <p v-if="event.error_code" class="mt-1 max-w-64 text-xs text-amber-700 dark:text-amber-400">{{ enumLabel('errors', event.error_code) }}</p>
                <div v-if="event.http_status || event.length" class="mt-1 text-xs text-gray-500"><span v-if="event.http_status">HTTP {{ event.http_status }}</span><span v-if="event.length"> · {{ event.length }} B</span></div>
                <p v-if="event.next_attempt_at" class="mt-1 text-xs text-gray-500">{{ t('admin.settings.codexTickets.monitor.retryAt', { time: formatTime(event.next_attempt_at) }) }}</p>
              </td>
              <td class="max-w-40 break-words px-3 py-3">{{ event.proxy_name || '—' }}<span v-if="event.proxy_id" class="block text-xs text-gray-500">#{{ event.proxy_id }}</span></td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-800 dark:text-gray-400">{{ t('admin.settings.codexTickets.monitor.emptyEvents') }}</p>
      <div v-if="filteredEvents.length" class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500">
        <span>{{ t('admin.settings.codexTickets.monitor.page', { page: eventPage, pages: eventPages, total: filteredEvents.length }) }}</span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="eventPage <= 1" @click="eventPage--">{{ t('admin.settings.codexTickets.monitor.previous') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="eventPage >= eventPages" @click="eventPage++">{{ t('admin.settings.codexTickets.monitor.next') }}</button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexTicketMonitor, type CodexTicketMonitorSnapshot } from '@/api/admin/settings'

const { t, locale } = useI18n()
const snapshot = ref<CodexTicketMonitorSnapshot | null>(null)
const loading = ref(false)
const error = ref(false)
const mode = ref('')
const accountSearch = ref('')
const statePage = ref(1)
const eventPage = ref(1)
const pageSize = 25
const knownEnums: Record<'statuses' | 'phases' | 'errors', ReadonlySet<string>> = {
  statuses: new Set(['ready', 'missing', 'capturing', 'verifying', 'cooldown', 'auth_blocked', 'revoked', 'error']),
  phases: new Set(['capture', 'verify', 'persist', 'cooldown', 'auth', 'revoke', 'use']),
  errors: new Set(['credential_missing', 'invalid_ticket', 'credential_mismatch', 'network_error', 'http_error', 'stream_invalid', 'stream_incomplete', 'model_mismatch', 'rate_limited', 'auth_failed', 'proxy_unavailable', 'persist_failed', 'cancelled']),
}

function enumLabel(kind: keyof typeof knownEnums, value: string) {
  if (!value) return '—'
  return t(`admin.settings.codexTickets.monitor.${kind}.${knownEnums[kind].has(value) ? value : 'unknown'}`)
}

function modeLabel(value: string) {
  return value === '292' || value === '332' ? `Codex ${value}` : t('admin.settings.codexTickets.monitor.statuses.unknown')
}

function statusClass(value: string) {
  if (value === 'ready') return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
  if (value === 'auth_blocked' || value === 'error' || value === 'revoked') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  if (value === 'cooldown') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleString(locale.value) : '—'
}

function matchesFilter(item: { mode: string; account_id: number; account_name: string }) {
  const query = accountSearch.value.trim().toLocaleLowerCase()
  return (!mode.value || item.mode === mode.value) && (!query || `${item.account_id} ${item.account_name}`.toLocaleLowerCase().includes(query))
}

const filteredStates = computed(() => (snapshot.value?.states ?? []).filter(matchesFilter))
const filteredEvents = computed(() => (snapshot.value?.events ?? []).filter(matchesFilter))
const statePages = computed(() => Math.max(1, Math.ceil(filteredStates.value.length / pageSize)))
const eventPages = computed(() => Math.max(1, Math.ceil(filteredEvents.value.length / pageSize)))
const statePageItems = computed(() => filteredStates.value.slice((statePage.value - 1) * pageSize, statePage.value * pageSize))
const eventPageItems = computed(() => filteredEvents.value.slice((eventPage.value - 1) * pageSize, eventPage.value * pageSize))
watch([mode, accountSearch], () => { statePage.value = 1; eventPage.value = 1 })
watch(statePages, pages => { statePage.value = Math.min(statePage.value, pages) })
watch(eventPages, pages => { eventPage.value = Math.min(eventPage.value, pages) })

let mounted = false
let revision = 0
let controller: AbortController | null = null
let timer: ReturnType<typeof setTimeout> | undefined
let refreshPending = false
const visible = () => document.visibilityState !== 'hidden'

function clearTimer() {
  if (timer !== undefined) clearTimeout(timer)
  timer = undefined
}

async function refresh() {
  if (!mounted || !visible()) return
  if (controller) {
    refreshPending = true
    return
  }
  clearTimer()
  loading.value = true
  const requestRevision = revision
  const requestController = new AbortController()
  controller = requestController
  try {
    const data = await getCodexTicketMonitor({ signal: requestController.signal })
    if (mounted && visible() && requestRevision === revision) {
      snapshot.value = { ...data, states: data.states.slice(0, 2048), events: data.events.slice(0, 300).sort((a, b) => Date.parse(b.time) - Date.parse(a.time)) }
      error.value = false
    }
  } catch {
    if (mounted && visible() && requestRevision === revision) error.value = true
  } finally {
    controller = null
    if (mounted) {
      loading.value = false
      if (visible()) {
        if (refreshPending) {
          refreshPending = false
          void refresh()
        } else {
          timer = setTimeout(() => { void refresh() }, 5000)
        }
      }
    }
  }
}

function visibilityChanged() {
  clearTimer()
  if (visible()) {
    void refresh()
  } else {
    revision++
    refreshPending = false
    controller?.abort()
  }
}

onMounted(() => {
  mounted = true
  document.addEventListener('visibilitychange', visibilityChanged)
  void refresh()
})
onBeforeUnmount(() => {
  mounted = false
  revision++
  clearTimer()
  controller?.abort()
  document.removeEventListener('visibilitychange', visibilityChanged)
})
</script>
