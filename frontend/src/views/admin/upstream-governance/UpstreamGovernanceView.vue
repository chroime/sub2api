<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errorKey, siteStateKeys } from './feedback'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImportPanel from './ImportPanel.vue'
import ConnectDialog from './ConnectDialog.vue'
import OnboardDialog from './OnboardDialog.vue'
import SiteEditDialog from './SiteEditDialog.vue'
import SiteOverview from './SiteOverview.vue'
import BalanceMonitorPanel from './BalanceMonitorPanel.vue'
import GovernanceHistory from './GovernanceHistory.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatGovernanceTime } from './format'
import api, {
  type Site,
  type Snapshot,
  type Binding,
  type Page,
  type GovernanceEvent,
  type Check,
  type ManagedKey,
} from '@/api/admin/upstream-governance'
import groupsAPI from '@/api/admin/groups'
import proxiesAPI from '@/api/admin/proxies'
import type { AdminGroup } from '@/types'
const { t } = useI18n()
const query = ref('')
const tab = ref<'import' | 'monitor'>('import')
const importBusy = ref(false)
const balanceBusy = ref(false)
const editBusy = ref(false)
let generation = 0
const sites = ref<Site[]>([]),
  active = ref<Site | null>(null),
  snapshot = ref<Snapshot | null>(null),
  bindings = ref<Binding[]>([])
const managedKeys = ref<ManagedKey[]>([]), importStateReady = ref(false)
const groups = ref<AdminGroup[]>([]),
  proxies = ref<{ id: number; name: string }[]>([])
const events = ref<Page<GovernanceEvent> | null>(null),
  checks = ref<Page<Check> | null>(null)
const busy = ref(false),
  error = ref(''),
  connecting = ref(false),
  onboarding = ref(false),
  editing = ref(false),
  deleting = ref(false)
const probe = ref<Binding | null>(null),
  probeAction = ref<'check' | 'monitor'>('check')
const working = computed(
  () => busy.value || importBusy.value || balanceBusy.value || editBusy.value,
)
const filteredSites = computed(() =>
  sites.value.filter((site) =>
    `${site.name} ${site.base_url}`
      .toLowerCase()
      .includes(query.value.toLowerCase()),
  ),
)
const healthySites = computed(
  () => sites.value.filter((site) => site.status === 'healthy').length,
)
const attentionSites = computed(
  () =>
    sites.value.filter(
      (site) => site.last_error || site.balance_monitor_status?.state === 'low',
    ).length,
)
function failure(e: unknown) {
  error.value = t(errorKey(e))
}
async function run(action: () => Promise<void>) {
  const request = generation
  busy.value = true
  error.value = ''
  try {
    await action()
  } catch (e) {
    if (request === generation) failure(e)
  } finally {
    if (request === generation) busy.value = false
  }
}
async function load() {
  const request = generation
  await run(async () => {
    const data = await api.list()
    if (request !== generation) return
    sites.value = data
    if (active.value)
      active.value = data.find((s) => s.id === active.value?.id) || null
  })
}
async function select(site: Site, collected?: Snapshot) {
  if (importBusy.value || balanceBusy.value || editBusy.value) return
  const request = ++generation
  connecting.value = false
  editing.value = false
  deleting.value = false
  probe.value = null
  active.value = site
  snapshot.value = null
  events.value = null
  checks.value = null
  bindings.value = []
  managedKeys.value = []
  importStateReady.value = false
  busy.value = true
  error.value = ''
  const results = await Promise.allSettled([
    collected ? Promise.resolve(collected) : api.catalog(site.id),
    api.bindings(site.id),
    api.events(site.id),
    api.checks(site.id),
    api.keys(site.id),
  ])
  if (request === generation && active.value?.id === site.id) {
    if (results[0].status === 'fulfilled') snapshot.value = results[0].value
    else if ((results[0].reason as { status?: number }).status !== 404)
      failure(results[0].reason)
    if (results[1].status === 'fulfilled') bindings.value = results[1].value
    else failure(results[1].reason)
    if (results[2].status === 'fulfilled') events.value = results[2].value
    else failure(results[2].reason)
    if (results[3].status === 'fulfilled') checks.value = results[3].value
    else failure(results[3].reason)
    if (results[4].status === 'fulfilled') managedKeys.value = results[4].value
    else failure(results[4].reason)
    importStateReady.value = results[1].status === 'fulfilled' && results[4].status === 'fulfilled'
    busy.value = false
  }
}
async function edited(site: Site) {
  editing.value = false
  editBusy.value = false
  siteCreated(site)
  await select(site)
}
async function sync() {
  if (!active.value) return
  const id = active.value.id
  const request = generation
  await run(async () => {
    try {
      const collected = await api.sync(id)
      const [list, history, currentBindings, currentKeys] = await Promise.all([api.list(), api.events(id), api.bindings(id), api.keys(id)])
      if (request !== generation) return
      snapshot.value = collected
      sites.value = list
      active.value = list.find((s) => s.id === id) || null
      events.value = history
      bindings.value = currentBindings
      managedKeys.value = currentKeys
      importStateReady.value = true
    } catch (e) {
      if (request === generation) importStateReady.value = false
      throw e
    }
  })
}
async function remove() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  await run(async () => {
    await api.remove(id)
    const list = await api.list()
    if (request !== generation) return
    active.value = null
    deleting.value = false
    sites.value = list
    snapshot.value = null
    bindings.value = []
    managedKeys.value = []
    importStateReady.value = false
    if (list[0]) await select(list[0])
  })
}
async function connected(site?: Site) {
  connecting.value = false
  if (!active.value || (site && site.id !== active.value.id)) return
  const updated = site || {
    ...active.value,
    has_credential: true,
    status: 'connected',
    last_error: '',
  }
  siteCreated(updated)
  active.value = updated
  await sync()
}
function siteCreated(site: Site) {
  sites.value = [...sites.value.filter((s) => s.id !== site.id), site]
}
function balanceSaved(site: Site) {
  siteCreated(site)
  if (active.value?.id === site.id) active.value = site
}
async function reloadBindings() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  try {
    const [value, keys] = await Promise.all([api.bindings(id), api.keys(id)])
    if (request === generation) {
      bindings.value = value
      managedKeys.value = keys
      importStateReady.value = true
    }
  } catch (e) {
    if (request === generation) {
      importStateReady.value = false
      failure(e)
    }
  }
}
async function onboarded(site: Site, collected: Snapshot) {
  onboarding.value = false
  const updated = {
    ...site,
    has_credential: true,
    status: 'healthy',
    last_error: '',
    last_sync_at: collected.created_at,
  }
  siteCreated(updated)
  await select(updated, collected)
}
async function page(kind: 'events' | 'checks', n: number) {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  await run(async () => {
    if (kind === 'events') {
      const value = await api.events(id, n)
      if (request === generation) events.value = value
    } else {
      const value = await api.checks(id, n)
      if (request === generation) checks.value = value
    }
  })
}
async function acknowledge(id: number) {
  if (!active.value) return
  const siteId = active.value.id,
    request = generation,
    eventPage = events.value?.page
  await run(async () => {
    await api.acknowledge(siteId, id)
    const value = await api.events(siteId, eventPage)
    if (request === generation) events.value = value
  })
}
function configure(binding: Binding, action: 'check' | 'monitor') {
  if (binding.account_id <= 0) return
  probe.value = {
    ...binding,
    probe_interval_minutes: binding.probe_interval_minutes || 30,
  }
  probeAction.value = action
}
async function submitProbe() {
  if (!probe.value || !active.value) return
  const id = active.value.id,
    request = generation,
    b = { ...probe.value },
    action = probeAction.value
  await run(async () => {
    if (action === 'check') {
      await api.check(id, b.id, b.probe_model)
      const value = await api.checks(id)
      if (request === generation) checks.value = value
    } else {
      await api.monitor(id, b.id, {
        enabled: b.probe_enabled,
        model: b.probe_model,
        interval_minutes: b.probe_interval_minutes,
      })
      const value = await api.bindings(id)
      if (request === generation) bindings.value = value
    }
    if (request === generation) probe.value = null
  })
}
onMounted(async () => {
  await load()
  await run(async () => {
    const [g, p] = await Promise.all([groupsAPI.getAll(), proxiesAPI.getAll()])
    groups.value = g
    proxies.value = p.map(({ id, name }) => ({ id, name }))
  })
  if (!active.value && sites.value[0]) await select(sites.value[0])
})
onUnmounted(() => {
  generation++
})
</script>
<template>
  <AppLayout>
    <div class="space-y-6 text-gray-900 dark:text-gray-100">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div class="flex items-start gap-3">
          <div
            class="rounded-xl bg-primary-50 p-3 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
          >
            <Icon name="server" size="lg" />
          </div>
          <div>
            <h2 class="text-xl font-semibold tracking-tight">
              {{ t('governance.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500">
              {{ t('governance.workspaceDescription') }}
            </p>
          </div>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="working" @click="load">
            <Icon name="refresh" size="sm" class="mr-2" />{{
              t('common.refresh')
            }}</button
          ><button
            id="governance-add-site"
            class="btn btn-primary"
            :disabled="working"
            @click="onboarding = true"
          >
            <Icon name="plus" size="sm" class="mr-2" />{{ t('governance.add') }}
          </button>
        </div>
      </header>
      <p
        v-if="error"
        role="alert"
        class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-900 dark:bg-red-900/10"
      >
        {{ error }}
      </p>
      <p
        v-if="!busy && !sites.length"
        class="rounded-2xl border border-dashed border-gray-200 p-12 text-center text-sm text-gray-500 dark:border-dark-600"
      >
        {{ t('governance.empty') }}
      </p>
      <div
        v-if="sites.length"
        class="grid min-w-0 gap-5 2xl:grid-cols-[248px_minmax(0,1fr)]"
      >
        <aside class="min-w-0 space-y-4 2xl:self-start">
          <div class="flex flex-wrap items-center gap-3 text-xs">
            <span class="font-semibold text-gray-700 dark:text-gray-200">{{
              t('governance.siteCount', { count: sites.length })
            }}</span
            ><span class="text-primary-700 dark:text-primary-300">{{
              t('governance.healthyCount', { count: healthySites })
            }}</span
            ><span
              v-if="attentionSites"
              class="text-amber-700 dark:text-amber-400"
              >{{
                t('governance.attentionCount', { count: attentionSites })
              }}</span
            >
          </div>
          <div class="relative">
            <Icon
              name="search"
              size="sm"
              class="absolute left-3 top-3 text-gray-400"
            /><input
              v-model="query"
              data-test="site-search"
              class="input w-full pl-9 text-sm"
              :placeholder="t('governance.searchSites')"
              :aria-label="t('governance.searchSites')"
            />
          </div>
          <nav
            class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-1"
            :aria-label="t('governance.upstreamSites')"
          >
            <button
              v-for="site in filteredSites"
              :key="site.id"
              :id="'governance-site-' + site.id"
              class="min-w-0 rounded-xl border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="
                active?.id === site.id
                  ? 'border-primary-400 bg-primary-50/60 shadow-sm dark:border-primary-600 dark:bg-primary-900/15'
                  : 'border-gray-200 bg-white hover:border-primary-300 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-primary-700'
              "
              :disabled="importBusy || balanceBusy || editBusy"
              :aria-current="active?.id === site.id ? 'true' : undefined"
              @click="select(site)"
            >
              <div class="flex items-center gap-2">
                <span
                  class="h-2 w-2 shrink-0 rounded-full"
                  :class="
                    site.last_error ||
                    site.balance_monitor_status?.state === 'low'
                      ? 'bg-amber-500'
                      : site.status === 'healthy'
                        ? 'bg-primary-500'
                        : 'bg-gray-300'
                  "
                /><strong class="truncate text-sm">{{ site.name }}</strong
                ><span class="ml-auto text-[10px] uppercase text-gray-400">{{
                  site.platform
                }}</span>
              </div>
              <p class="mt-2 truncate text-xs text-gray-500">
                {{ site.base_url }}
              </p>
              <p
                class="mt-3 text-xs"
                :class="
                  site.last_error
                    ? 'text-amber-700 dark:text-amber-400'
                    : 'text-gray-500'
                "
              >
                {{ t('governance.' + (siteStateKeys[site.status] || 'unknown'))
                }}<span v-if="site.balance_monitor_status?.state === 'low'">
                  · {{ t('governance.balanceState_low') }}</span
                >
              </p>
              <time
                class="mt-1 block whitespace-nowrap text-[11px] tabular-nums text-gray-400"
                >{{ formatGovernanceTime(site.last_sync_at) }}</time
              >
            </button>
          </nav>
          <p
            v-if="!filteredSites.length"
            class="p-4 text-center text-xs text-gray-400"
          >
            {{ t('governance.noMatchingSites') }}
          </p>
        </aside>
        <div v-if="active" class="min-w-0 space-y-5">
          <section
            class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-600 dark:bg-dark-800"
          >
            <div class="mb-5 flex flex-wrap items-center gap-3">
              <div class="mr-auto min-w-0">
                <h3 class="text-lg font-semibold">{{ active.name }}</h3>
                <p class="mt-1 break-all text-xs text-gray-500">
                  {{ active.base_url }}
                  <span class="ml-2 uppercase">{{ active.platform }}</span>
                </p>
              </div>
              <button
                class="btn btn-secondary text-sm"
                :disabled="working"
                @click="editing = true"
              >
                {{ t('common.edit') }}</button
              ><button
                id="governance-reconnect"
                class="btn btn-secondary text-sm"
                :disabled="working"
                @click="connecting = true"
              >
                {{
                  t(
                    active.has_credential
                      ? 'governance.reconnect'
                      : 'governance.connect',
                  )
                }}</button
              ><button
                id="governance-collect"
                class="btn btn-primary text-sm"
                :disabled="working || !active.has_credential"
                @click="sync"
              >
                <Icon
                  name="refresh"
                  size="sm"
                  class="mr-2"
                  :class="busy ? 'animate-spin' : ''"
                />{{
                  busy ? t('common.loading') : t('governance.sync')
                }}</button
              ><button
                class="rounded-lg p-2 text-gray-400 hover:bg-red-50 hover:text-red-600 focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-red-900/20"
                :disabled="working"
                :aria-label="t('common.delete')"
                @click="deleting = true"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <p
              v-if="active.status === 'reauth_required'"
              role="alert"
              class="mb-4 rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
            >
              {{ t('governance.reauth') }}
            </p>
            <p
              v-else-if="active.last_error"
              role="alert"
              class="mb-4 text-sm text-red-600"
            >
              {{ t(errorKey({ reason: active.last_error })) }}
            </p>
            <SiteOverview
              :site="active"
              :snapshot="snapshot"
              :binding-count="
                bindings.filter((binding) => binding.account_id > 0).length
              "
            />
          </section>
          <div
            class="rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-600 dark:bg-dark-800"
          >
            <nav
              class="flex gap-6 border-b border-gray-100 px-5 dark:border-dark-700"
              :aria-label="t('governance.workspaceSections')"
            >
              <button
                id="governance-import-tab"
                type="button"
                class="flex items-center gap-2 border-b-2 py-4 text-sm font-medium"
                :class="
                  tab === 'import'
                    ? 'border-primary-600 text-primary-700 dark:text-primary-300'
                    : 'border-transparent text-gray-500'
                "
                :aria-pressed="tab === 'import'"
                @click="tab = 'import'"
              >
                <Icon name="download" size="sm" />{{
                  t('governance.groupImport')
                }}
              </button>
              <button
                id="governance-monitor-tab"
                type="button"
                class="flex items-center gap-2 border-b-2 py-4 text-sm font-medium"
                :class="
                  tab === 'monitor'
                    ? 'border-primary-600 text-primary-700 dark:text-primary-300'
                    : 'border-transparent text-gray-500'
                "
                :aria-pressed="tab === 'monitor'"
                @click="tab = 'monitor'"
              >
                <Icon name="bell" size="sm" />{{
                  t('governance.monitoringAndHistory')
                }}<span
                  v-if="active.balance_monitor_status?.state === 'low'"
                  class="h-2 w-2 rounded-full bg-red-500"
                />
              </button>
            </nav>
            <div v-show="tab === 'import'" class="p-5">
              <ImportPanel
                v-if="snapshot && importStateReady"
                :key="active.id"
                :site-id="active.id"
                :site-base-url="active.base_url"
                :site-platform="active.platform"
                :bindings="bindings"
                :managed-keys="managedKeys"
                :snapshot="snapshot"
                :groups="groups"
                :disabled="busy || balanceBusy"
                @busy="importBusy = $event"
                @applied="reloadBindings"
              />
              <p v-else class="py-12 text-center text-sm text-gray-400">
                {{ busy ? t('common.loading') : t(snapshot ? 'governance.importStateUnavailable' : 'governance.noSnapshot') }}
              </p>
            </div>
            <div v-show="tab === 'monitor'" class="space-y-6 p-5">
              <BalanceMonitorPanel
                :key="active.id"
                :site="active"
                :unit="snapshot?.catalog.account?.unit"
                :disabled="working"
                @saved="balanceSaved"
                @busy="balanceBusy = $event"
              /><GovernanceHistory
                :bindings="bindings"
                :events="events"
                :checks="checks"
                :disabled="working"
                @configure="configure"
                @acknowledge="acknowledge"
                @page="page"
              />
            </div>
          </div>
        </div>
        <div
          v-else
          class="flex min-h-72 items-center justify-center rounded-2xl border border-dashed border-gray-200 text-sm text-gray-400 dark:border-dark-600"
        >
          {{ t('governance.chooseSite') }}
        </div>
      </div>
      <OnboardDialog
        v-if="onboarding"
        :proxies="proxies"
        @close="onboarding = false"
        @created="siteCreated"
        @completed="onboarded"
      />
      <ConnectDialog
        v-if="connecting && active"
        :site-id="active.id"
        @close="connecting = false"
        @connected="connected"
      />
      <SiteEditDialog
        v-if="editing && active"
        :key="active.id"
        :site="active"
        :proxies="proxies"
        @close="editing = false"
        @saved="edited"
        @busy="editBusy = $event"
      />
      <BaseDialog
        :show="deleting"
        :title="t('common.delete')"
        :show-close-button="!busy"
        :close-on-escape="!busy"
        @close="deleting = false"
        ><p>{{ t('governance.deleteNotice') }}</p>
        <p v-if="error" class="text-red-600">{{ error }}</p>
        <button class="btn btn-danger mt-4" :disabled="busy" @click="remove">
          {{ t('common.delete') }}
        </button></BaseDialog
      >
      <BaseDialog
        :show="!!probe"
        :title="
          t(probeAction === 'check' ? 'governance.check' : 'governance.monitor')
        "
        :show-close-button="!busy"
        :close-on-escape="!busy"
        @close="probe = null"
        ><form v-if="probe" class="space-y-4" @submit.prevent="submitProbe">
          <p class="text-amber-700">{{ t('governance.billable') }}</p>
          <p v-if="error" class="text-red-600">{{ error }}</p>
          <label class="block"
            >{{ t('governance.model')
            }}<input
              v-model="probe.probe_model"
              class="input w-full"
              :required="
                probeAction === 'check' || probe.probe_enabled
              " /></label
          ><template v-if="probeAction === 'monitor'"
            ><label class="flex gap-2"
              ><input v-model="probe.probe_enabled" type="checkbox" />{{
                t('governance.enableMonitor')
              }}</label
            ><label class="block"
              >{{ t('governance.interval')
              }}<input
                v-model.number="probe.probe_interval_minutes"
                class="input w-full"
                type="number"
                min="15"
                max="1440"
                required /></label></template
          ><button class="btn btn-primary" :disabled="busy">
            {{
              t(
                probeAction === 'check'
                  ? 'governance.confirmCheck'
                  : 'common.save',
              )
            }}
          </button>
        </form></BaseDialog
      >
    </div></AppLayout
  >
</template>
