<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'
import {
  defaultAccountName,
  defaultImportConfig,
  defaultModelSelections,
  importAccountConfig,
  type ModelSelections,
} from './import-config'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import ImportSettingsPanel from './ImportSettingsPanel.vue'
import TransportSelect from './TransportSelect.vue'
import TargetGroupSelect from './TargetGroupSelect.vue'
import { initialTransport, transportUnavailable } from './providers'
import PriceDetails from './PriceDetails.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type Snapshot,
  type Selection,
  type Preview,
  type ApplyResult,
  type Transport,
  type SiteInput,
  type KeySelection,
  type Binding,
} from '@/api/admin/upstream-governance'
const props = defineProps<{
  siteId: number
  siteBaseUrl: string
  sitePlatform?: SiteInput['platform']
  bindings?: Pick<Binding, 'remote_group_id' | 'platform'>[]
  managedKeys?: KeySelection[]
  disabled?: boolean
  snapshot: Snapshot
  groups: {
    id: number
    name: string
    platform: string
    rate_multiplier: number
  }[]
}>()
const emit = defineEmits<{ applied: []; busy: [value: boolean] }>()
const { t } = useI18n()
type GroupChoice = Omit<Selection, 'platform'> & {
  selected: boolean
  platform: Transport | ''
}
const choices = ref<Record<string, GroupChoice>>({})
const preview = ref<Preview | null>(null)
const result = ref<ApplyResult | null>(null)
const busy = ref(false),
  keyBusy = ref(false),
  error = ref('')
const query = ref(''),
  platformFilter = ref<Transport | ''>(''),
  selectedOnly = ref(false)
const bulkTarget = ref(0),
  bulkMessage = ref(''),
  expandedGroup = ref('')
const config = ref(defaultImportConfig()),
  quotaEnabled = ref(true),
  modelsReady = ref(false)
const modelSelections = ref<ModelSelections>(defaultModelSelections())
let generation = 0
const working = computed(() => busy.value || keyBusy.value)
const resolvedGroups = computed(() =>
  props.snapshot.catalog.groups.map((group) => ({
    ...group,
    platform: choices.value[group.id]?.platform || group.platform,
  })),
)
watch(working, (value) => emit('busy', value), { flush: 'sync' })
watch(
  () => [props.siteId, props.snapshot.id],
  () => {
    generation++
    choices.value = Object.fromEntries(
      (props.snapshot.catalog.groups || []).map((group) => [
        group.id,
        {
          selected: false,
          remote_group_id: group.id,
          platform: initialTransport(group.id, group.platform, props.bindings, props.managedKeys, props.sitePlatform),
          local_group_id: 0,
          account_name: defaultAccountName(
            props.siteBaseUrl,
            group.resolved_rate_multiplier ?? 1,
          ),
          cost_multiplier: group.resolved_rate_multiplier ?? 1,
        },
      ]),
    )
    preview.value = null
    result.value = null
    bulkTarget.value = 0
    bulkMessage.value = ''
    error.value = ''
    query.value = ''
    platformFilter.value = ''
    selectedOnly.value = false
    busy.value = false
    config.value = defaultImportConfig()
    quotaEnabled.value = true
    modelsReady.value = false
    modelSelections.value = defaultModelSelections()
  },
  { immediate: true },
)
onUnmounted(() => {
  generation++
  emit('busy', false)
})
const selected = computed(() =>
  Object.values(choices.value).filter((choice) => choice.selected),
)
const visibleGroups = computed(() =>
  props.snapshot.catalog.groups.filter((group) => {
    const choice = choices.value[group.id]!
    return (
      (!selectedOnly.value || choice.selected) &&
      (!platformFilter.value || choice.platform === platformFilter.value) &&
      `${group.name} ${group.id} ${(group.models || []).join(' ')}`
        .toLowerCase()
        .includes(query.value.toLowerCase())
    )
  }),
)
const allSelected = computed({
  get: () =>
    !!visibleGroups.value.length &&
    visibleGroups.value.every((group) => choices.value[group.id]!.selected),
  set: (value: boolean) => {
    visibleGroups.value.forEach((group) => {
      choices.value[group.id]!.selected = value
    })
  },
})
const keySelections = computed(() =>
  selected.value.map((choice) => ({
    remote_group_id: choice.remote_group_id,
    platform: choice.platform,
  })),
)
const unresolved = computed(() =>
  selected.value.filter(
    (choice) =>
      !choice.platform ||
      !!transportUnavailable(choice.platform, props.sitePlatform, props.snapshot.catalog.groups.find(group => group.id === choice.remote_group_id)?.platform) ||
      !props.groups.some(
        (group) =>
          group.id === choice.local_group_id &&
          (group.platform === choice.platform ||
            group.platform === 'composite'),
      ),
  ),
)
const emptyWhitelist = computed(() =>
  selected.value.some(
    (choice) =>
      choice.platform &&
      modelSelections.value[choice.platform].enabled &&
      !modelSelections.value[choice.platform].models.length,
  ),
)
function assignTarget() {
  const group = props.groups.find((item) => item.id === bulkTarget.value)
  if (!group) return
  let skipped = 0
  for (const choice of selected.value) {
    if (
      choice.platform &&
      (group.platform === 'composite' || group.platform === choice.platform)
    )
      choice.local_group_id = group.id
    else {
      choice.local_group_id = 0
      skipped++
    }
  }
  bulkMessage.value = skipped
    ? t('governance.bulkUnresolved', { count: skipped })
    : t('governance.bulkAssigned', { count: selected.value.length })
}
function changeRate(choice: GroupChoice, event: Event) {
  const previous = defaultAccountName(props.siteBaseUrl, choice.cost_multiplier)
  choice.cost_multiplier = Number((event.target as HTMLInputElement).value)
  if (choice.account_name === previous)
    choice.account_name = defaultAccountName(
      props.siteBaseUrl,
      choice.cost_multiplier,
    )
}
function remap() {
  preview.value = null
  result.value = null
}
async function prepare() {
  if (working.value || props.disabled || !selected.value.length) return
  if (
    unresolved.value.length ||
    selected.value.some(
      (choice) =>
        !choice.account_name.trim() ||
        !Number.isFinite(choice.cost_multiplier) ||
        choice.cost_multiplier < 0,
    )
  ) {
    error.value = t('governance.mappingIncomplete')
    return
  }
  if (!modelsReady.value || emptyWhitelist.value) {
    error.value = t('governance.emptyWhitelist')
    return
  }
  const request = generation
  busy.value = true
  error.value = ''
  try {
    const value = await api.preview(props.siteId, {
      selections: selected.value.map(
        ({ selected: _selected, ...selection }) => ({
          ...selection,
          platform: selection.platform as Transport,
          account_config: importAccountConfig(
            config.value,
            selection.platform as Transport,
            modelSelections.value[selection.platform as Transport],
            quotaEnabled.value,
          ),
        }),
      ),
    })
    if (request !== generation) return
    preview.value = value
    result.value = null
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) busy.value = false
  }
}
async function apply() {
  if (working.value || props.disabled || !preview.value) return
  if (Date.parse(preview.value.expires_at) <= Date.now()) {
    preview.value = null
    error.value = t('governance.stale')
    return
  }
  const request = generation
  busy.value = true
  error.value = ''
  try {
    const value = await api.apply(props.siteId, preview.value.id)
    if (request !== generation) return
    result.value = value
    emit('applied')
  } catch (e) {
    if (request !== generation) return
    error.value = t(errorKey(e))
    if (
      (e as { status?: number }).status === 409 &&
      (e as { reason?: string }).reason !== 'site_busy'
    )
      preview.value = null
  } finally {
    if (request === generation) busy.value = false
  }
}
const value = (number: number | null | undefined) =>
  number == null ? t('governance.unknown') : number
const catalogWarnings: Record<string, string> = {
  sub2api_model_plaza_unavailable: 'plazaUnavailable',
  sub2api_channels_unavailable: 'channelsUnavailable',
  sub2api_channels_pricing_unavailable: 'channelsPricingUnavailable',
  newapi_channels_not_exposed: 'channelsNotExposed',
  newapi_pricing_unavailable: 'pricingUnavailable',
  sub2api_complex_pricing_not_flattened: 'complexPricingNotFlattened',
  newapi_complex_pricing_not_flattened: 'complexPricingNotFlattened',
}
</script>
<template>
  <section class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold">
          {{ t('governance.groupImport') }}
        </h3>
        <p class="mt-1 text-xs text-gray-500">
          {{ t('governance.importWorkflow') }}
        </p>
      </div>
      <span class="text-xs tabular-nums text-gray-500"
        >{{ t('governance.snapshot') }} ·
        {{ formatGovernanceTime(snapshot.created_at) }}</span
      >
    </div>
    <details
      v-if="snapshot.catalog.warnings?.length"
      class="rounded-xl border border-amber-200 bg-amber-50/70 px-4 py-3 text-xs text-amber-800 dark:border-amber-800/50 dark:bg-amber-900/10 dark:text-amber-300"
    >
      <summary class="cursor-pointer font-medium">
        {{
          t('governance.catalogNotices', {
            count: snapshot.catalog.warnings.length,
          })
        }}
      </summary>
      <p
        v-for="warning in snapshot.catalog.warnings"
        :key="warning"
        class="mt-2"
      >
        {{
          catalogWarnings[warning]
            ? t('governance.' + catalogWarnings[warning])
            : warning
        }}
      </p>
    </details>
    <p
      v-if="!snapshot.catalog.groups?.length"
      class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-600"
    >
      {{ t('governance.emptyCatalog') }}
    </p>
    <p
      v-if="error"
      role="alert"
      class="rounded-lg bg-red-50 p-3 text-sm text-red-600 dark:bg-red-900/10"
    >
      {{ error }}
    </p>
    <form v-show="!preview" class="space-y-4" @submit.prevent="prepare">
      <fieldset :disabled="working || disabled" class="min-w-0 space-y-3">
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative min-w-48 flex-1">
            <Icon
              name="search"
              size="sm"
              class="absolute left-3 top-3 text-gray-400"
            /><input
              v-model="query"
              data-test="group-search"
              class="input w-full pl-9 text-sm"
              :placeholder="t('governance.searchGroups')"
              :aria-label="t('governance.searchGroups')"
            />
          </div>
          <TransportSelect
            v-model="platformFilter"
            data-test="protocol-filter"
            class="w-full text-sm sm:w-56"
            :disabled="working || disabled"
            allow-all
          /><label class="flex items-center gap-2 text-xs text-gray-500"
            ><input v-model="selectedOnly" type="checkbox" />{{
              t('governance.onlySelected')
            }}</label
          >
        </div>
        <div
          class="flex flex-wrap items-center gap-3 rounded-xl bg-primary-50/70 px-4 py-3 dark:bg-primary-900/10"
        >
          <label class="flex items-center gap-2 text-sm font-medium"
            ><input
              id="governance-select-all"
              v-model="allSelected"
              data-test="select-all"
              type="checkbox"
              :indeterminate="selected.length > 0 && !allSelected"
            />{{
              t('governance.selectAll', { count: visibleGroups.length })
            }}</label
          ><span
            class="mr-auto text-xs text-primary-700 dark:text-primary-300"
            >{{
              t('governance.selectedCount', { count: selected.length })
            }}</span
          ><TargetGroupSelect
            id="governance-bulk-target"
            v-model="bulkTarget"
            data-test="bulk-target"
            class="w-full min-w-48 text-sm sm:w-72"
            :groups="groups"
            :disabled="working || disabled"
            :placeholder="t('governance.bulkTarget')"
            :aria-label="t('governance.bulkTarget')"
          /><button
            id="governance-assign-target"
            data-test="assign-target"
            type="button"
            class="btn btn-secondary text-sm"
            :disabled="!bulkTarget || !selected.length"
            @click="assignTarget"
          >
            {{ t('governance.assignSelected') }}
          </button>
        </div>
        <p
          class="text-xs text-gray-500"
        >{{ t('governance.targetGroupHint') }}</p>
        <p
          v-if="bulkMessage"
          role="status"
          class="text-xs text-primary-700 dark:text-primary-300"
        >
          {{ bulkMessage }}
        </p>
        <div
          class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-600"
        >
          <table class="w-full text-left text-sm">
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900">
              <tr>
                <th class="w-9 px-3 py-3">
                  <span class="sr-only">{{ t('governance.choose') }}</span>
                </th>
                <th class="min-w-40 px-3 py-3 font-medium">
                  {{ t('governance.remoteGroup') }}
                </th>
                <th class="min-w-28 px-3 py-3 font-medium">
                  {{ t('governance.resolvedRate') }}
                </th>
                <th class="min-w-36 px-3 py-3 font-medium">
                  {{ t('governance.transport') }}
                </th>
                <th class="min-w-52 px-3 py-3 font-medium">
                  {{ t('governance.localGroup') }}
                </th>
                <th class="w-16 px-3 py-3 font-medium">
                  {{ t('governance.details') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <template v-for="remote in visibleGroups" :key="remote.id">
                <tr
                  :class="
                    choices[remote.id]!.selected
                      ? 'bg-primary-50/30 dark:bg-primary-900/10'
                      : ''
                  "
                >
                  <td class="px-3 py-3">
                    <input
                      v-model="choices[remote.id]!.selected"
                      data-test="select"
                      type="checkbox"
                      :aria-label="remote.name"
                    />
                  </td>
                  <td class="px-3 py-3">
                    <span class="font-medium">{{ remote.name }}</span>
                    <p class="mt-1 text-xs text-gray-400">
                      #{{ remote.id }} ·
                      {{
                        t('governance.modelSelectedCount', {
                          count: remote.models?.length || 0,
                        })
                      }}
                    </p>
                  </td>
                  <td class="px-3 py-3">
                    <span
                      class="font-semibold tabular-nums text-primary-700 dark:text-primary-300"
                      >{{ value(remote.resolved_rate_multiplier)
                      }}<span v-if="remote.resolved_rate_multiplier != null"
                        >×</span
                      ></span
                    >
                    <p
                      v-if="remote.peak_rate_enabled"
                      class="mt-1 text-[11px] text-amber-600"
                    >
                      {{ t('governance.peak') }}
                      {{ value(remote.peak_rate_multiplier) }}×
                    </p>
                  </td>
                  <td class="px-3 py-3">
                    <TransportSelect
                      v-model="choices[remote.id]!.platform"
                      data-test="platform"
                      :site-platform="sitePlatform"
                      :remote-platform="remote.platform"
                      :disabled="working || disabled"
                      @update:model-value="
                        choices[remote.id]!.local_group_id = 0
                      "
                    />
                  </td>
                  <td class="px-3 py-3">
                    <TargetGroupSelect
                      v-model="choices[remote.id]!.local_group_id"
                      data-test="target"
                      class="w-full text-sm"
                      :groups="groups"
                      :transport="choices[remote.id]!.platform"
                      :aria-label="
                        t('governance.localGroup') + ' ' + remote.name
                      "
                      :disabled="working || disabled || !choices[remote.id]!.selected || !choices[remote.id]!.platform"
                    />
                  </td>
                  <td class="px-3 py-3">
                    <button
                      data-test="group-details"
                      type="button"
                      class="rounded p-2 text-gray-500 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700"
                      :aria-label="t('governance.details') + ' ' + remote.name"
                      :aria-expanded="expandedGroup === remote.id"
                      @click="
                        expandedGroup =
                          expandedGroup === remote.id ? '' : remote.id
                      "
                    >
                      <Icon
                        :name="
                          expandedGroup === remote.id
                            ? 'chevronUp'
                            : 'chevronDown'
                        "
                        size="sm"
                      />
                    </button>
                  </td>
                </tr>
                <tr v-if="expandedGroup === remote.id">
                  <td
                    colspan="6"
                    class="bg-gray-50/70 px-6 py-4 dark:bg-dark-900/70"
                  >
                    <div class="grid gap-4 lg:grid-cols-2">
                      <div class="space-y-3">
                        <label class="block text-xs text-gray-500"
                          >{{ t('governance.accountName')
                          }}<input
                            v-model="choices[remote.id]!.account_name"
                            data-test="account-name"
                            class="input mt-1 w-full text-sm"
                            maxlength="100" /></label
                        ><label class="block text-xs text-gray-500"
                          >{{ t('governance.costRate')
                          }}<input
                            :value="choices[remote.id]!.cost_multiplier"
                            class="input mt-1 w-full text-sm"
                            type="number"
                            min="0"
                            step="any"
                            @input="changeRate(choices[remote.id]!, $event)"
                        /></label>
                        <p class="text-xs text-gray-500">
                          {{ t('governance.baseRate') }}
                          {{ value(remote.rate_multiplier) }} ·
                          {{ t('governance.overrideRate') }}
                          {{ value(remote.user_rate_multiplier)
                          }}<span v-if="remote.peak_rate_enabled">
                            · {{ remote.peak_start }}–{{
                              remote.peak_end
                            }}</span
                          >
                        </p>
                      </div>
                      <div class="space-y-3">
                        <p class="break-all text-xs text-gray-500">
                          {{ t('governance.models') }}:
                          {{
                            remote.models?.join(', ') || t('governance.unknown')
                          }}
                        </p>
                        <PriceDetails :prices="remote.prices" />
                        <p class="text-xs text-gray-400">
                          {{ t('governance.source') }}: {{ remote.source }}
                        </p>
                      </div>
                    </div>
                  </td>
                </tr>
              </template>
              <tr v-if="!visibleGroups.length">
                <td colspan="6" class="p-6 text-center text-gray-400">
                  {{ t('governance.noMatchingGroups') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </fieldset>
      <ImportSettingsPanel
        :key="siteId + ':' + snapshot.id"
        v-model:config="config"
        v-model:models="modelSelections"
        v-model:quota-enabled="quotaEnabled"
        :groups="resolvedGroups"
        :disabled="working || disabled"
        @ready="modelsReady = $event"
      />
      <div
        class="flex flex-wrap items-center gap-3 rounded-xl border border-primary-100 bg-primary-50/50 p-4 dark:border-primary-900/40 dark:bg-primary-900/10"
      >
        <div class="mr-auto">
          <p class="text-sm font-medium">
            {{ t('governance.selectedCount', { count: selected.length }) }}
          </p>
          <p
            v-if="unresolved.length"
            role="status"
            class="mt-1 text-xs text-amber-700 dark:text-amber-400"
          >
            {{
              t('governance.mappingUnresolved', { count: unresolved.length })
            }}
          </p>
          <p v-else class="mt-1 text-xs text-gray-500">
            {{ t('governance.previewHint') }}
          </p>
          <p
            v-if="emptyWhitelist"
            role="alert"
            class="mt-1 text-xs text-amber-700"
          >
            {{ t('governance.emptyWhitelist') }}
          </p>
        </div>
        <button
          id="governance-preview"
          data-test="preview"
          class="btn btn-primary"
          :disabled="
            working ||
            disabled ||
            !selected.length ||
            !!unresolved.length ||
            !modelsReady ||
            emptyWhitelist
          "
        >
          {{ t('governance.preview')
          }}<Icon name="arrowRight" size="sm" class="ml-2" />
        </button>
      </div>
    </form>
    <section
      v-if="preview"
      class="space-y-4 rounded-xl border border-primary-200 p-5 dark:border-primary-800"
    >
      <div class="flex flex-wrap justify-between gap-2">
        <h4 class="font-semibold">{{ t('governance.frozen') }}</h4>
        <span class="text-xs tabular-nums text-gray-500"
          >{{ t('governance.expires') }}
          {{ formatGovernanceTime(preview.expires_at) }}</span
        >
      </div>
      <p class="text-xs text-gray-500">
        {{ t('governance.saleNotice') }} {{ t('governance.nameAndNoteHint') }}
      </p>
      <article
        v-for="row in preview.rows"
        :key="row.marker"
        class="space-y-2 rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-900"
      >
        <h5 class="font-semibold">
          {{ row.remote_group.name }} → {{ row.target.name }}
          <span class="text-xs font-normal text-gray-500">{{
            row.selection.platform
          }}</span>
        </h5>
        <p class="text-xs text-gray-500">
          {{ t('governance.before') }}:
          {{
            row.existing
              ? `${row.existing.name} (#${row.existing.id}) · ${row.existing.cost_multiplier}×`
              : t('governance.newAccount')
          }}
        </p>
        <p class="break-all">
          {{ row.selection.account_name }} · {{ t('governance.costRate') }}
          {{ row.selection.cost_multiplier }}×
        </p>
        <p class="text-xs text-gray-500">
          {{ t('governance.saleRate') }} {{ row.target.sale_multiplier }}× ·
          {{ t('governance.resolvedRate') }}
          {{ value(row.remote_group.resolved_rate_multiplier) }}×
        </p>
        <p v-if="row.selection.account_config" class="text-xs text-gray-500">
          {{ t('governance.concurrency') }}
          {{ row.selection.account_config.concurrency }} ·
          {{ t('governance.dailyQuota') }}
          {{ row.selection.account_config.quota_daily_limit }} ·
          {{ t('governance.weeklyQuota') }}
          {{ row.selection.account_config.quota_weekly_limit }} ·
          {{ t('governance.totalQuota') }}
          {{ row.selection.account_config.quota_limit }}
        </p>
        <PriceDetails
          data-test="preview-prices"
          :prices="row.remote_group.prices"
        />
        <p
          v-if="row.will_create_key"
          class="text-xs text-amber-700 dark:text-amber-400"
        >
          {{ t('governance.keyWarning') }}
        </p>
      </article>
      <div class="flex flex-wrap gap-3">
        <button
          data-test="apply"
          class="btn btn-primary"
          :disabled="working || disabled"
          @click="apply"
        >
          {{ result ? t('governance.retry') : t('governance.apply') }}</button
        ><button
          class="btn btn-secondary"
          :disabled="working || disabled"
          @click="remap"
        >
          {{ t('governance.remap') }}
        </button>
      </div>
    </section>
    <section
      v-if="result"
      aria-live="polite"
      class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-600"
    >
      <h4 class="font-semibold">{{ t('governance.results') }}</h4>
      <p class="text-xs text-gray-500">{{ t('governance.partialNotice') }}</p>
      <p
        v-for="item in result.items"
        :key="item.remote_group_id + item.platform"
        class="text-sm"
        :class="
          item.error ? 'text-red-600' : 'text-primary-700 dark:text-primary-300'
        "
      >
        {{ item.remote_group_id }} / {{ item.platform }}: {{ item.status }}
        <span v-if="item.account_id">#{{ item.account_id }}</span>
        {{ item.error }}
      </p>
    </section>
    <ManagedKeysPanel
      :key="siteId"
      :site-id="siteId"
      :snapshot-id="snapshot.id"
      :groups="snapshot.catalog.groups"
      :selections="keySelections"
      :disabled="busy || disabled || !!preview"
      @busy="keyBusy = $event"
    />
    <details
      v-if="snapshot.catalog.channels?.length"
      class="rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-600"
    >
      <summary class="cursor-pointer font-medium">
        {{ t('governance.channels') }} · {{ snapshot.catalog.channels.length }}
      </summary>
      <p
        v-for="channel in snapshot.catalog.channels"
        :key="channel.name"
        class="mt-2 text-xs text-gray-500"
      >
        {{ channel.name }} · {{ channel.group_ids?.join(', ') }} ·
        {{ channel.models?.join(', ') }}
      </p>
    </details>
  </section>
</template>
