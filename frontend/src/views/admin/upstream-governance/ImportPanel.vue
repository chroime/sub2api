<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import api, {
  type Snapshot,
  type Selection,
  type Preview,
  type ApplyResult,
  type Transport,
} from '@/api/admin/upstream-governance'
const props = defineProps<{
  siteId: number
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
type GroupChoice = Omit<Selection, 'platform'> & { selected: boolean; platform: Transport | '' }
const choices = ref<Record<string, GroupChoice>>({})
const preview = ref<Preview | null>(null)
const result = ref<ApplyResult | null>(null)
const busy = ref(false)
const error = ref('')
const bulkTarget = ref(0)
const bulkMessage = ref('')
function inferTransport(platform: string): Transport | '' {
  const normalized = platform.toLowerCase()
  if (normalized === 'grok') return 'openai'
  return ['openai', 'anthropic', 'gemini'].includes(normalized) ? normalized as Transport : ''
}
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
watch(
  () => [props.siteId, props.snapshot.id],
  () => {
    choices.value = Object.fromEntries(
      (props.snapshot.catalog.groups || []).map((g) => [
        g.id,
        {
          selected: false,
          remote_group_id: g.id,
          platform: inferTransport(g.platform),
          local_group_id: 0,
          account_name: g.name,
          cost_multiplier: g.resolved_rate_multiplier ?? 1,
        },
      ]),
    )
    preview.value = null
    result.value = null
    bulkTarget.value = 0
    bulkMessage.value = ''
    error.value = ''
  },
  { immediate: true },
)
const selected = computed(() =>
  Object.values(choices.value).filter((c) => c.selected),
)
const allSelected = computed({
  get: () => !!props.snapshot.catalog.groups.length && selected.value.length === props.snapshot.catalog.groups.length,
  set: (value: boolean) => { Object.values(choices.value).forEach(choice => { choice.selected = value }) },
})
const keySelections = computed(() => selected.value.map(choice => ({
  remote_group_id: choice.remote_group_id,
  platform: choice.platform,
})))
function compatible(choice: GroupChoice) {
  return !!choice.platform && props.groups.some(group => group.id === choice.local_group_id && (group.platform === choice.platform || group.platform === 'composite'))
}
const unresolved = computed(() => selected.value.filter(choice => !compatible(choice)))
function assignTarget() {
  const group = props.groups.find(group => group.id === bulkTarget.value)
  if (!group) return
  let skipped = 0
  for (const choice of selected.value) {
    if (choice.platform && (group.platform === 'composite' || group.platform === choice.platform)) choice.local_group_id = group.id
    else { choice.local_group_id = 0; skipped++ }
  }
  bulkMessage.value = skipped ? t('governance.bulkUnresolved', { count: skipped }) : t('governance.bulkAssigned', { count: selected.value.length })
}
function remap() {
  preview.value = null
  result.value = null
}
async function prepare() {
  if (busy.value || props.disabled || !selected.value.length) return
  if (unresolved.value.length || selected.value.some(choice => !choice.account_name.trim() || !Number.isFinite(choice.cost_multiplier) || choice.cost_multiplier < 0)) {
    error.value = t('governance.mappingIncomplete')
    return
  }
  error.value = ''
  busy.value = true
  try {
    preview.value = await api.preview(props.siteId, {
      selections: selected.value.map(
        ({ selected: _selected, ...selection }) => ({ ...selection, platform: selection.platform as Transport }),
      ),
    })
    result.value = null
  } catch (e) {
    error.value = t(errorKey(e))
  } finally {
    busy.value = false
  }
}
async function apply() {
  if (busy.value || props.disabled || !preview.value) return
  if (Date.parse(preview.value.expires_at) <= Date.now()) {
    preview.value = null
    error.value = t('governance.stale')
    return
  }
  busy.value = true
  error.value = ''
  try {
    result.value = await api.apply(props.siteId, preview.value.id)
    emit('applied')
  } catch (e) {
    const status = (e as { status?: number }).status
    error.value = t(errorKey(e))
    if (status === 409 && (e as { reason?: string }).reason !== 'site_busy')
      preview.value = null
  } finally {
    busy.value = false
  }
}
const priceUnits: Record<string, string> = { usd_per_token: 'usdPerMillionTokens', usd_per_request: 'usdPerRequest', newapi_ratio: 'newapiRatio' }
const value = (n: number | null | undefined) =>
  n == null ? t('governance.unknown') : n
const tokenPrice = (n: number | null, unit: string) => n == null ? t('governance.unknown') : unit === 'usd_per_token' ? Number((n * 1_000_000).toPrecision(12)) : n
const accountAmount = (n: number | null) => n == null ? t('governance.unknown') : `${n} ${props.snapshot.catalog.account?.unit || t('governance.unknown')}`
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
  <section class="space-y-4">
    <h3 class="text-lg font-semibold">{{ t('governance.catalog') }}</h3>
    <p class="text-sm text-gray-500">
      {{ t('governance.snapshot') }}: {{ snapshot.created_at }}
    </p>
    <section v-if="snapshot.catalog.account" class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800">
      <h4 class="font-semibold">{{ t('governance.upstreamAccount') }} · {{ snapshot.catalog.account.username || snapshot.catalog.account.email || '#' + snapshot.catalog.account.user_id }}</h4>
      <dl class="mt-3 grid gap-3 sm:grid-cols-3">
        <div><dt class="text-sm text-gray-500">{{ t('governance.balance') }}</dt><dd data-test="account-balance" class="text-lg font-semibold">{{ accountAmount(snapshot.catalog.account.balance) }}</dd></div>
        <div><dt class="text-sm text-gray-500">{{ t('governance.frozenBalance') }}</dt><dd data-test="account-frozen">{{ accountAmount(snapshot.catalog.account.frozen_balance) }}</dd></div>
        <div><dt class="text-sm text-gray-500">{{ t('governance.usedBalance') }}</dt><dd data-test="account-used">{{ accountAmount(snapshot.catalog.account.used_balance) }}</dd></div>
      </dl>
      <p class="mt-2 text-xs text-gray-500">{{ t('governance.source') }}: {{ snapshot.catalog.account.source }} · {{ t('governance.balanceUnitsHint') }}</p>
    </section>
    <p
      v-for="warning in snapshot.catalog.warnings"
      :key="warning"
      class="text-amber-700"
    >
      {{ catalogWarnings[warning] ? t('governance.' + catalogWarnings[warning]) : warning }}
    </p>
    <p v-if="!snapshot.catalog.groups?.length">
      {{ t('governance.emptyCatalog') }}
    </p>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <form v-if="!preview" class="space-y-4" @submit.prevent="prepare">
      <fieldset class="flex flex-wrap items-end gap-3 rounded-xl border p-4 dark:border-dark-600" :disabled="busy || disabled">
        <label class="mr-auto flex items-center gap-2 self-center font-medium"><input id="governance-select-all" v-model="allSelected" data-test="select-all" type="checkbox" :indeterminate="selected.length > 0 && !allSelected" />{{ t('governance.selectAll', { count: snapshot.catalog.groups.length }) }}</label>
        <span class="self-center text-sm text-gray-500">{{ t('governance.selectedCount', { count: selected.length }) }}</span>
        <label for="governance-bulk-target" class="min-w-56 text-sm">{{ t('governance.bulkTarget') }}
          <select id="governance-bulk-target" v-model="bulkTarget" data-test="bulk-target" class="input mt-1 w-full">
            <option :value="0">{{ t('governance.choose') }}</option>
            <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }} · {{ group.platform }} · {{ t('governance.saleRate') }} {{ group.rate_multiplier }}</option>
          </select>
        </label>
        <button id="governance-assign-target" data-test="assign-target" type="button" class="btn btn-secondary" :disabled="!bulkTarget || !selected.length" @click="assignTarget">{{ t('governance.assignSelected') }}</button>
        <p v-if="bulkMessage" role="status" class="w-full text-sm text-amber-700">{{ bulkMessage }}</p>
      </fieldset>
      <fieldset
        v-for="remote in snapshot.catalog.groups"
        :key="remote.id"
        class="rounded-xl border border-gray-200 p-4 dark:border-dark-600"
        :disabled="busy || disabled"
      >
        <label class="flex gap-2 font-semibold"
          ><input
            v-model="choices[remote.id]!.selected"
            data-test="select"
            type="checkbox"
          />{{ remote.name }}</label
        >
        <dl class="my-2 grid gap-2 text-sm sm:grid-cols-3">
          <div>
            {{ t('governance.baseRate') }}: {{ value(remote.rate_multiplier) }}
          </div>
          <div>
            {{ t('governance.overrideRate') }}:
            {{ value(remote.user_rate_multiplier) }}
          </div>
          <div>
            {{ t('governance.resolvedRate') }}:
            {{ value(remote.resolved_rate_multiplier) }}
          </div>
          <div>{{ t('governance.source') }}: {{ remote.source }}</div>
          <div v-if="remote.peak_rate_enabled">
            {{ t('governance.peak') }}: {{ remote.peak_start }}–{{
              remote.peak_end
            }}
            / {{ value(remote.peak_rate_multiplier) }}
          </div>
        </dl>
        <p class="break-words text-sm">
          {{ t('governance.models') }}:
          {{ remote.models?.join(', ') || t('governance.unknown') }}
        </p>
        <details class="my-2 text-sm">
          <summary>{{ t('governance.prices') }}</summary>
          <p v-if="remote.prices?.length" class="my-1 text-gray-500">{{ t('governance.priceMultiplierHint') }}</p>
          <p v-if="!remote.prices?.length">{{ t('governance.unknown') }}</p>
          <p v-for="(price, i) in remote.prices" :key="i">
            {{ price.model }} · {{ price.platform || t('governance.unknown') }} · {{ priceUnits[price.unit] ? t('governance.' + priceUnits[price.unit]) : (price.unit || t('governance.unknown')) }} · {{ t('governance.input') }}
            {{ tokenPrice(price.input, price.unit) }} / {{ t('governance.output') }}
            {{ tokenPrice(price.output, price.unit) }} / {{ t('governance.request') }}
            {{ value(price.per_request) }}<span v-if="price.details" class="block break-words text-gray-500">{{ t('governance.priceDetails') }}: {{ JSON.stringify(price.details) }}</span>
          </p>
        </details>
        <div
          v-if="choices[remote.id]!.selected"
          class="grid gap-3 sm:grid-cols-2"
        >
          <p v-if="!choices[remote.id]!.platform" role="status" class="text-sm text-amber-700 sm:col-span-2">{{ t('governance.chooseTransport') }}</p>
          <label
            >{{ t('governance.transport')
            }}<select
              v-model="choices[remote.id]!.platform"
              data-test="platform"
              class="input w-full"
              required
              @change="choices[remote.id]!.local_group_id = 0"
            >
              <option disabled value="">{{ t('governance.choose') }}</option>
              <option v-for="p in ['openai', 'anthropic', 'gemini']" :key="p">
                {{ p }}
              </option>
            </select></label
          >
          <label
            >{{ t('governance.localGroup')
            }}<select
              v-model="choices[remote.id]!.local_group_id"
              data-test="target"
              class="input w-full"
              required
            >
              <option disabled :value="0">{{ t('governance.choose') }}</option>
              <option
                v-for="g in groups.filter(
                  (g) => g.platform === choices[remote.id]!.platform || g.platform === 'composite',
                )"
                :key="g.id"
                :value="g.id"
              >
                {{ g.name }} · {{ t('governance.saleRate') }}
                {{ g.rate_multiplier }}
              </option>
            </select></label
          >
          <label
            >{{ t('governance.accountName')
            }}<input
              v-model="choices[remote.id]!.account_name"
              class="input w-full"
              required
              maxlength="100"
          /></label>
          <label
            >{{ t('governance.costRate')
            }}<input
              v-model.number="choices[remote.id]!.cost_multiplier"
              class="input w-full"
              type="number"
              min="0"
              step="any"
              required
          /></label>
        </div>
      </fieldset>
      <p v-if="unresolved.length" role="status" class="text-sm text-amber-700">{{ t('governance.mappingUnresolved', { count: unresolved.length }) }}</p>
      <button
        id="governance-preview"
        data-test="preview"
        class="btn btn-primary"
        :disabled="
          busy ||
          disabled ||
          !selected.length ||
          !!unresolved.length
        "
      >
        {{ t('governance.preview') }}
      </button>
    </form>
    <section v-else class="space-y-3 rounded-xl border border-blue-300 p-4">
      <h4 class="font-semibold">{{ t('governance.frozen') }}</h4>
      <p>{{ t('governance.expires') }}: {{ preview.expires_at }}</p>
      <p>{{ t('governance.saleNotice') }}</p>
      <p class="text-amber-700">{{ t('governance.keyLimits') }}</p>
      <article
        v-for="row in preview.rows"
        :key="row.marker"
        class="border-b py-3 dark:border-dark-600"
      >
        <h5 class="font-semibold">
          {{ row.remote_group.name }} → {{ row.target.name }} ({{
            row.selection.platform
          }})
        </h5>
        <p>
          {{ t('governance.before') }}:
          {{
            row.existing
              ? `${row.existing.name} (#${row.existing.id}) · ${t('governance.costRate')} ${row.existing.cost_multiplier} · ${t('governance.localGroup')} ${row.existing.group_ids.join(', ')}`
              : t('governance.newAccount')
          }}
        </p>
        <p>
          {{ t('governance.after') }}: {{ row.selection.account_name }} ·
          {{ t('governance.costRate') }} {{ row.selection.cost_multiplier }} ·
          {{ t('governance.localGroup') }} {{ row.target.name }}
        </p>
        <p>{{ t('governance.saleRate') }}: {{ row.target.sale_multiplier }}</p>
        <p>{{ t('governance.resolvedRate') }}: {{ value(row.remote_group.resolved_rate_multiplier) }}</p>
        <details data-test="preview-prices" class="my-2 text-sm">
          <summary>{{ t('governance.prices') }}</summary>
          <p v-if="!row.remote_group.prices?.length">{{ t('governance.unknown') }}</p>
          <p v-else class="my-1 text-gray-500">{{ t('governance.priceMultiplierHint') }}</p>
          <p v-for="(price, i) in row.remote_group.prices" :key="i">
            {{ price.model }} · {{ price.platform || t('governance.unknown') }} · {{ priceUnits[price.unit] ? t('governance.' + priceUnits[price.unit]) : (price.unit || t('governance.unknown')) }} · {{ t('governance.input') }}
            <span data-test="price-input">{{ tokenPrice(price.input, price.unit) }}</span> / {{ t('governance.output') }}
            <span data-test="price-output">{{ tokenPrice(price.output, price.unit) }}</span> / {{ t('governance.request') }}
            {{ value(price.per_request) }}<span v-if="price.details" class="block break-words text-gray-500">{{ t('governance.priceDetails') }}: {{ JSON.stringify(price.details) }}</span>
          </p>
        </details>
        <p>
          {{ t('governance.models') }}:
          {{ row.remote_group.models?.join(', ') }}
        </p>
        <p v-if="row.will_create_key" class="text-amber-700">
          {{ t('governance.keyWarning') }}
        </p>
      </article>
      <button
        data-test="apply"
        class="btn btn-primary"
        :disabled="busy || disabled"
        @click="apply"
      >
        {{ result ? t('governance.retry') : t('governance.apply') }}
      </button>
      <button
        class="btn btn-secondary ml-2"
        :disabled="busy || disabled"
        @click="remap"
      >
        {{ t('governance.remap') }}
      </button>
    </section>
    <section v-if="result" aria-live="polite">
      <h4 class="font-semibold">{{ t('governance.results') }}</h4>
      <p>{{ t('governance.partialNotice') }}</p>
      <p
        v-for="item in result.items"
        :key="item.remote_group_id + item.platform"
      >
        {{ item.remote_group_id }} / {{ item.platform }}: {{ item.status }}
        <span v-if="item.account_id">#{{ item.account_id }}</span>
        {{ item.error }}
      </p>
    </section>
    <ManagedKeysPanel :key="siteId" :site-id="siteId" :snapshot-id="snapshot.id" :groups="snapshot.catalog.groups" :selections="keySelections" :disabled="busy || disabled || !!preview" @busy="busy = $event" />
    <details>
      <summary>{{ t('governance.channels') }}</summary>
      <p v-for="channel in snapshot.catalog.channels" :key="channel.name">
        {{ channel.name }} · {{ channel.group_ids?.join(', ') }} ·
        {{ channel.models?.join(', ') }}
      </p>
    </details>
  </section>
</template>
