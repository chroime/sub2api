<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type {
  Binding,
  Check,
  GovernanceEvent,
  Page,
  Transport,
} from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
import { eventKeys } from './feedback'
const props = defineProps<{
  bindings: Binding[]
  groups?: { id: number; name: string }[]
  remoteGroups?: { id: string; name: string }[]
  events: Page<GovernanceEvent> | null
  checks: Page<Check> | null
  disabled?: boolean
  mode?: 'bindings' | 'history'
}>()
const emit = defineEmits<{
  configure: [binding: Binding, action: 'check' | 'monitor']
  acknowledge: [id: number]
  page: [kind: 'events' | 'checks', page: number]
}>()
const { t } = useI18n()
const historyTab = ref<'events' | 'checks'>('events')
const remoteNames = computed(() => new Map(props.remoteGroups?.map(group => [group.id, group.name])))
type RateSnapshot = { Resolved?: number | null }
function rateSnapshot(value: string): RateSnapshot | null {
  try {
    const parsed = JSON.parse(value) as RateSnapshot
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
}
function rateText(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : '—'
}
function eventSummary(event: GovernanceEvent): string {
  const group = remoteNames.value.get(event.resource) || `#${event.resource}`
  if (event.kind === 'rate_changed') {
    const before = rateSnapshot(event.before)?.Resolved
    const after = rateSnapshot(event.after)?.Resolved
    if (typeof before === 'number' && typeof after === 'number' && Number.isFinite(before) && Number.isFinite(after)) {
      const delta = after - before
      if (delta > 0 && before !== 0) return t('governance.rateChangedUpSummary', { group, before: rateText(before), after: rateText(after), delta: rateText(delta), percent: (Math.round(delta / before * 10000) / 100).toString() })
      if (delta < 0 && before !== 0) return t('governance.rateChangedDownSummary', { group, before: rateText(before), after: rateText(after), delta: rateText(Math.abs(delta)), percent: (Math.round(Math.abs(delta) / before * 10000) / 100).toString() })
      return t('governance.rateChangedStableSummary', { group, before: rateText(before), after: rateText(after) })
    }
  }
  const summaryKeys: Record<string, string> = { group_added: 'groupAddedSummary', group_removed: 'groupRemovedSummary', group_changed: 'groupChangedSummary', price_changed: 'priceChangedSummary', models_changed: 'modelsChangedSummary', channels_changed: 'channelsChangedSummary' }
  return t(`governance.${summaryKeys[event.kind] || 'eventChangeSummary'}`, { group })
}
</script>
<template>
  <section class="space-y-6">
    <section v-if="mode !== 'history'" class="min-w-0 space-y-3">
      <div>
        <h3 class="font-semibold">
          {{ t('governance.bindings') }}
          <span class="ml-1 text-sm font-normal text-gray-400">{{
            bindings.length
          }}</span>
        </h3>
        <p class="mt-1 text-xs text-gray-500">{{ t('governance.billable') }}</p>
      </div>
      <p
        v-if="!bindings.length"
        class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600"
      >
        {{ t('governance.noBindings') }}
      </p>
      <article
        v-for="binding in bindings"
        :key="binding.id"
        class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"
      >
        <PlatformIcon :platform="binding.platform as Transport" size="md" />
        <div class="mr-auto min-w-0 flex-1 break-words">
          <p class="text-sm font-medium" data-test="binding-upstream">
            {{ remoteNames.get(binding.remote_group_id) || t('governance.remoteGroup') }}
            <span class="ml-2 text-xs font-normal text-gray-400">#{{ binding.remote_group_id }}</span>
          </p>
          <p class="mt-1 break-all text-xs text-gray-500" data-test="binding-account">
            {{ t('governance.localAccount') }}：
            <template v-if="binding.account_id > 0">
              {{ binding.account_name || t('governance.accountNameUnavailable') }}
              <span class="ml-1 text-gray-400">#{{ binding.account_id }}</span>
              <span v-if="binding.account_deleted" class="ml-2 rounded bg-gray-100 px-1.5 py-0.5 text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ t('governance.accountDeleted') }}</span>
            </template>
            <template v-else>{{ t('governance.pendingImport') }}</template>
          </p>
          <p class="mt-1 text-xs text-gray-500" data-test="binding-targets">
            {{ t('governance.localGroup') }}：
            {{ (binding.local_group_ids?.length ? binding.local_group_ids : [binding.local_group_id]).map(id => groups?.find(group => group.id === id)?.name || '#' + id).join('、') }}
          </p>
          <p class="mt-1 text-xs text-gray-500">
            {{
              binding.probe_enabled
                ? t('governance.monitorOn')
                : t('governance.monitorOff')
            }}
            · {{ binding.probe_model || '—' }}
          </p>
        </div>
        <div class="flex w-full flex-wrap gap-2 sm:w-auto">
        <button
          class="btn btn-secondary text-xs"
          :disabled="disabled || binding.account_id <= 0 || binding.account_deleted"
          @click="emit('configure', binding, 'check')"
        >
          {{ t('governance.check') }}</button
        ><button
          class="btn btn-secondary text-xs"
          :disabled="disabled || binding.account_id <= 0 || binding.account_deleted"
          @click="emit('configure', binding, 'monitor')"
        >
          {{ t('governance.monitor') }}
        </button>
        </div>
      </article>
    </section>
    <nav v-if="mode === 'history'" class="flex flex-wrap gap-2" :aria-label="t('governance.historyTitle')"><button v-for="kind in (['events', 'checks'] as const)" :key="kind" type="button" class="rounded-lg px-3 py-2 text-sm" :class="historyTab === kind ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500'" :aria-pressed="historyTab === kind" @click="historyTab = kind">{{ t('governance.' + kind) }}</button></nav>
    <section v-if="events && mode !== 'bindings' && (mode !== 'history' || historyTab === 'events')" class="space-y-3">
      <h3 class="font-semibold">
        {{ t('governance.events') }}
        <span class="ml-1 text-sm font-normal text-gray-400">{{
          events.total
        }}</span>
      </h3>
      <p
        v-if="!events.items.length"
        class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600"
      >
        {{ t('governance.noEvents') }}
      </p>
      <article
        v-for="event in events.items"
        :key="event.id"
        class="rounded-xl border border-gray-200 p-4 dark:border-dark-600"
      >
        <div class="flex flex-wrap items-center gap-3">
          <span class="mr-auto text-sm font-medium"
            >{{ t('governance.' + (eventKeys[event.kind] || 'eventChange')) }}
            <span class="ml-2 text-xs font-normal text-gray-500">{{
              event.resource
            }}</span></span
          ><time class="text-xs tabular-nums text-gray-400">{{
            formatGovernanceTime(event.created_at)
          }}</time
          ><button
            class="rounded-md px-2 py-1 text-xs text-primary-700 hover:bg-primary-50 disabled:text-gray-400 dark:text-primary-300 dark:hover:bg-primary-900/20"
            :disabled="disabled || event.acknowledged"
            @click="emit('acknowledge', event.id)"
          >
            {{
              t(
                event.acknowledged
                  ? 'governance.acknowledged'
                  : 'governance.ack',
              )
            }}
          </button>
        </div>
        <p data-test="event-summary" class="mt-3 text-sm leading-6 text-gray-700 dark:text-gray-200">{{ eventSummary(event) }}</p>
        <details
          v-if="event.before || event.after"
          class="mt-2 text-xs text-gray-500"
        >
          <summary class="cursor-pointer">
            {{ t('governance.rawChangeData') }}
          </summary>
          <p class="mt-2 break-all">
            {{ t('governance.before') }}: {{ event.before || '—' }}
          </p>
          <p class="mt-1 break-all">
            {{ t('governance.after') }}: {{ event.after || '—' }}
          </p>
        </details>
      </article>
      <div
        v-if="events.total"
        class="flex items-center justify-end gap-3 text-xs text-gray-500"
      >
        <button
          class="btn btn-secondary text-xs"
          :disabled="disabled || events.page <= 1"
          @click="emit('page', 'events', events.page - 1)"
        >
          {{ t('governance.previous') }}</button
        ><span>{{ events.page }} / {{ Math.max(1, events.pages) }}</span
        ><button
          class="btn btn-secondary text-xs"
          :disabled="disabled || events.page >= events.pages"
          @click="emit('page', 'events', events.page + 1)"
        >
          {{ t('governance.next') }}
        </button>
      </div>
    </section>
    <section v-if="checks && mode !== 'bindings' && (mode !== 'history' || historyTab === 'checks')" class="space-y-3">
      <h3 class="font-semibold">
        {{ t('governance.checks') }}
        <span class="ml-1 text-sm font-normal text-gray-400">{{
          checks.total
        }}</span>
      </h3>
      <p
        v-if="!checks.items.length"
        class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600"
      >
        {{ t('governance.noChecks') }}
      </p>
      <div
        v-if="checks.items.length"
        class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-600"
      >
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900">
            <tr>
              <th class="p-3 font-medium">{{ t('governance.models') }}</th>
              <th class="p-3 font-medium">{{ t('governance.checkResult') }}</th>
              <th class="p-3 font-medium">{{ t('governance.latency') }}</th>
              <th class="p-3 font-medium">{{ t('governance.checkTime') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="check in checks.items" :key="check.id">
              <td class="p-3">
                {{ check.model
                }}<span class="ml-2 text-xs text-gray-400"
                  >#{{ check.binding_id }}</span
                >
              </td>
              <td
                class="p-3"
                :class="
                  check.success
                    ? 'text-primary-700 dark:text-primary-300'
                    : 'text-red-600'
                "
              >
                {{
                  t(check.success ? 'governance.success' : 'governance.failed')
                }}<span v-if="check.error_code" class="block text-xs">{{
                  check.error_code
                }}</span>
              </td>
              <td class="p-3 tabular-nums">{{ check.latency_ms }} ms</td>
              <td
                class="whitespace-nowrap p-3 text-xs tabular-nums text-gray-500"
              >
                {{ formatGovernanceTime(check.created_at) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-if="checks.total"
        class="flex items-center justify-end gap-3 text-xs text-gray-500"
      >
        <button
          class="btn btn-secondary text-xs"
          :disabled="disabled || checks.page <= 1"
          @click="emit('page', 'checks', checks.page - 1)"
        >
          {{ t('governance.previous') }}</button
        ><span>{{ checks.page }} / {{ Math.max(1, checks.pages) }}</span
        ><button
          class="btn btn-secondary text-xs"
          :disabled="disabled || checks.page >= checks.pages"
          @click="emit('page', 'checks', checks.page + 1)"
        >
          {{ t('governance.next') }}
        </button>
      </div>
    </section>
  </section>
</template>
