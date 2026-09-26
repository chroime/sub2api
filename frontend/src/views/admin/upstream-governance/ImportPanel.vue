<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { errorKey } from './feedback'
import { useI18n } from 'vue-i18n'
import api, {
  type Snapshot,
  type Selection,
  type Preview,
  type ApplyResult,
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
const choices = ref<Record<string, Selection & { selected: boolean }>>({})
const preview = ref<Preview | null>(null)
const result = ref<ApplyResult | null>(null)
const busy = ref(false)
const error = ref('')
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
watch(
  () => props.snapshot.id,
  () => {
    choices.value = Object.fromEntries(
      (props.snapshot.catalog.groups || []).map((g) => [
        g.id,
        {
          selected: false,
          remote_group_id: g.id,
          platform: '' as Selection['platform'],
          local_group_id: 0,
          account_name: g.name,
          cost_multiplier: g.resolved_rate_multiplier ?? 1,
        },
      ]),
    )
    preview.value = null
    result.value = null
  },
  { immediate: true },
)
const selected = computed(() =>
  Object.values(choices.value).filter((c) => c.selected),
)
function remap() {
  preview.value = null
  result.value = null
}
async function prepare() {
  error.value = ''
  busy.value = true
  try {
    preview.value = await api.preview(props.siteId, {
      selections: selected.value.map(
        ({ selected: _selected, ...selection }) => selection,
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
  if (!preview.value) return
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
const priceUnits: Record<string, string> = { usd_per_token: 'usdPerToken', usd_per_request: 'usdPerRequest', newapi_ratio: 'newapiRatio' }
const value = (n: number | null | undefined) =>
  n == null ? t('governance.unknown') : n
</script>
<template>
  <section class="space-y-4">
    <h3 class="text-lg font-semibold">{{ t('governance.catalog') }}</h3>
    <p class="text-sm text-gray-500">
      {{ t('governance.snapshot') }}: {{ snapshot.created_at }}
    </p>
    <p
      v-for="warning in snapshot.catalog.warnings"
      :key="warning"
      class="text-amber-700"
    >
      {{ warning }}
    </p>
    <p v-if="!snapshot.catalog.groups?.length">
      {{ t('governance.emptyCatalog') }}
    </p>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <form v-if="!preview" class="space-y-4" @submit.prevent="prepare">
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
          <p v-if="!remote.prices?.length">{{ t('governance.unknown') }}</p>
          <p v-for="(price, i) in remote.prices" :key="i">
            {{ price.model }} · {{ priceUnits[price.unit] ? t('governance.' + priceUnits[price.unit]) : (price.unit || t('governance.unknown')) }} · {{ t('governance.input') }}
            {{ value(price.input) }} / {{ t('governance.output') }}
            {{ value(price.output) }} / {{ t('governance.request') }}
            {{ value(price.per_request) }}<span v-if="price.details" class="block break-words text-gray-500">{{ t('governance.priceDetails') }}: {{ JSON.stringify(price.details) }}</span>
          </p>
        </details>
        <div
          v-if="choices[remote.id]!.selected"
          class="grid gap-3 sm:grid-cols-2"
        >
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
      <button
        class="btn btn-primary"
        :disabled="
          busy ||
          disabled ||
          !selected.length ||
          selected.some((s) => !s.local_group_id || !s.platform)
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
    <details>
      <summary>{{ t('governance.channels') }}</summary>
      <p v-for="channel in snapshot.catalog.channels" :key="channel.name">
        {{ channel.name }} · {{ channel.group_ids?.join(', ') }} ·
        {{ channel.models?.join(', ') }}
      </p>
    </details>
  </section>
</template>
