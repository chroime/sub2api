<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errorKey, siteStateKeys } from './feedback'
import { intervalValidationKey } from './interval'
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
import GovernanceSitesOverview from './GovernanceSitesOverview.vue'
import AutomationPolicyPanel from './AutomationPolicyPanel.vue'
import ReconciliationPanel from './ReconciliationPanel.vue'
import BalanceHealthPanel from './BalanceHealthPanel.vue'
import RechargePlanPanel from './RechargePlanPanel.vue'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type Site,
  type Snapshot,
  type Binding,
  type Page,
  type GovernanceEvent,
  type Check,
  type ManagedKey,
  type KeySelection,
  type AutomationConfiguration,
  type BalanceHealth,
} from '@/api/admin/upstream-governance'
import groupsAPI from '@/api/admin/groups'
import proxiesAPI from '@/api/admin/proxies'
import type { AdminGroup } from '@/types'
const { t } = useI18n()
const tab = ref<'overview' | 'import' | 'monitor' | 'history'>('overview')
const showOverview = ref(true)
const automation = ref<AutomationConfiguration | null>(null), balanceHealth = ref<BalanceHealth | null>(null)
const automationBusy = ref(false), reconciliationBusy = ref(false), keyBusy = ref(false), rechargeBusy = ref(false)
const keysOpen = ref(false), keySelections = ref<KeySelection[]>([]), reconciliationEpoch = ref(0)
const importBusy = ref(false)
const balanceBusy = ref(false)
const editBusy = ref(false)
let generation = 0
let healthGeneration = 0
const sites = ref<Site[]>([]),
  active = ref<Site | null>(null),
  snapshot = ref<Snapshot | null>(null),
  overviewSnapshot = ref<Snapshot | null>(null),
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
  () => busy.value || importBusy.value || balanceBusy.value || editBusy.value || automationBusy.value || reconciliationBusy.value || keyBusy.value || rechargeBusy.value,
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
  if (importBusy.value || balanceBusy.value || editBusy.value || automationBusy.value || reconciliationBusy.value || keyBusy.value || rechargeBusy.value) return
  const request = ++generation
  connecting.value = false
  editing.value = false
  deleting.value = false
  probe.value = null
  if (active.value?.id !== site.id) tab.value = 'overview'
  active.value = site
  showOverview.value = false
  keysOpen.value = false
  keySelections.value = []
  automation.value = null
  balanceHealth.value = null
  snapshot.value = null
  overviewSnapshot.value = null
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
    api.automation(site.id),
    api.balanceHealth(site.id),
  ])
  if (request === generation && active.value?.id === site.id) {
    if (results[0].status === 'fulfilled') snapshot.value = overviewSnapshot.value = results[0].value
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
    if (results[5].status === 'fulfilled') automation.value = results[5].value
    if (results[6].status === 'fulfilled') balanceHealth.value = results[6].value
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
      overviewSnapshot.value = collected
      sites.value = list
      active.value = list.find((s) => s.id === id) || null
      events.value = history
      bindings.value = currentBindings
      managedKeys.value = currentKeys
      importStateReady.value = true
      await reloadHealth()
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
    overviewSnapshot.value = null
    bindings.value = []
    managedKeys.value = []
    importStateReady.value = false
    showOverview.value = true
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
  void reloadHealth()
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
  if (binding.account_id <= 0 || binding.account_deleted) return
  probe.value = {
    ...binding,
    probe_interval_minutes: binding.probe_interval_minutes || 30,
  }
  probeAction.value = action
}
async function submitProbe() {
  if (!probe.value || !active.value) return
  if (probeAction.value === 'monitor') {
    const intervalError = intervalValidationKey(probe.value.probe_interval_minutes)
    if (intervalError) { error.value = t(intervalError); return }
  }
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
function openKeys(selections: KeySelection[] = []) {
  if (working.value || !active.value) return
  keySelections.value = [...selections]
  keysOpen.value = true
}
async function reloadHealth() {
  if (!active.value) return
  const id = active.value.id, request = generation, healthRequest = ++healthGeneration
  try {
    const [health, latest] = await Promise.all([
      api.balanceHealth(id),
      api.catalog(id).catch(e => {
        if ((e as { status?: number }).status === 404) return null
        throw e
      }),
    ])
    if (request === generation && healthRequest === healthGeneration) {
      balanceHealth.value = health
      overviewSnapshot.value = latest
    }
  } catch (e) {
    if (request === generation && healthRequest === healthGeneration) {
      balanceHealth.value = null
      overviewSnapshot.value = null
      failure(e)
    }
  }
}
async function reloadAutomation() {
  if (!active.value) return
  const id = active.value.id, request = generation
  try { const value = await api.automation(id); if (request === generation) automation.value = value }
  catch (e) { if (request === generation) failure(e) }
}
function automationSaved(value: AutomationConfiguration) {
  automation.value = value
  reconciliationEpoch.value++
}
async function reconciled() {
  if (!active.value) return
  const id = active.value.id, request = generation
  await reloadBindings()
  await reloadHealth()
  try { const value = await api.events(id); if (request === generation) events.value = value }
  catch (e) { if (request === generation) failure(e) }
}
onMounted(async () => {
  await load()
  await run(async () => {
    const [g, p] = await Promise.all([groupsAPI.getAll(), proxiesAPI.getAll()])
    groups.value = g
    proxies.value = p.map(({ id, name }) => ({ id, name }))
  })
})
onUnmounted(() => {
  generation++
})
</script>
<template>
  <AppLayout>
    <div class="min-w-0 space-y-6 text-gray-900 dark:text-gray-100">
      <header class="flex flex-wrap items-start justify-between gap-4"><div class="min-w-0"><h2 class="text-xl font-semibold tracking-tight">{{ t('governance.title') }}</h2><p class="mt-1 max-w-3xl text-sm leading-relaxed text-gray-500">{{ t('governance.workspaceDescription') }}</p></div><div class="flex flex-wrap gap-2"><button class="btn btn-secondary" :disabled="working" @click="load"><Icon name="refresh" size="sm" class="mr-2" />{{ t('common.refresh') }}</button><button id="governance-add-site" class="btn btn-primary" :disabled="working" @click="onboarding = true"><Icon name="plus" size="sm" class="mr-2" />{{ t('governance.add') }}</button></div></header>
      <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-900 dark:bg-red-900/10">{{ error }}</p>
      <p v-if="!busy && !sites.length" class="rounded-xl border border-dashed border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('governance.empty') }}</p>
      <GovernanceSitesOverview v-show="showOverview" v-if="sites.length" :sites="sites" :disabled="importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || keyBusy || rechargeBusy" @select="select" />
      <section v-if="active" v-show="!showOverview" class="min-w-0 space-y-5">
        <div class="flex flex-wrap items-center gap-3"><button id="governance-back-sites" type="button" class="btn btn-secondary text-sm" :disabled="working" @click="showOverview = true">{{ t('governance.allSites') }}</button><div class="min-w-0 flex-1"><h3 class="break-words text-lg font-semibold">{{ active.name }}</h3><p class="mt-1 break-all text-xs text-gray-500">{{ active.base_url }} · {{ active.platform }}</p></div><span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs dark:bg-dark-700">{{ t('governance.' + (siteStateKeys[active.status] || 'unknown')) }}</span></div>
        <div class="flex flex-wrap gap-2"><button id="governance-collect" class="btn btn-primary text-sm" :disabled="working || !active.has_credential" @click="sync"><Icon name="refresh" size="sm" class="mr-2" :class="busy ? 'animate-spin' : ''" />{{ busy ? t('common.loading') : t('governance.sync') }}</button><button id="governance-view-keys" data-test="view-keys" class="btn btn-secondary text-sm" :disabled="working" @click="openKeys()">{{ t('governance.viewKeys') }}</button><button class="btn btn-secondary text-sm" :disabled="working" @click="editing = true">{{ t('common.edit') }}</button><button id="governance-reconnect" class="btn btn-secondary text-sm" :disabled="working" @click="connecting = true">{{ t(active.has_credential ? 'governance.reconnect' : 'governance.connect') }}</button><button class="ml-auto rounded-lg p-2 text-gray-500 hover:bg-red-50 hover:text-red-600 focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-red-900/20" :disabled="working" :aria-label="t('common.delete')" @click="deleting = true"><Icon name="trash" size="sm" /></button></div>
        <p v-if="active.status === 'reauth_required'" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.reauth') }}</p><p v-else-if="active.last_error" role="alert" class="text-sm text-red-600">{{ t(errorKey({ reason: active.last_error })) }}</p>
        <div class="min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
          <nav class="grid grid-cols-2 gap-1 border-b border-gray-100 p-2 dark:border-dark-700 sm:flex sm:flex-wrap" :aria-label="t('governance.workspaceSections')">
            <button v-for="entry in ([{ value: 'overview', id: 'governance-overview-tab', label: 'overviewTab' }, { value: 'import', id: 'governance-import-tab', label: 'groupImport' }, { value: 'monitor', id: 'governance-monitor-tab', label: 'automationTab' }, { value: 'history', id: 'governance-history-tab', label: 'historyTitle' }] as const)" :id="entry.id" :key="entry.value" type="button" class="min-h-11 rounded-lg px-4 py-2 text-sm transition-colors" :class="tab === entry.value ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500 hover:bg-gray-50 dark:hover:bg-dark-700'" :aria-pressed="tab === entry.value" @click="tab = entry.value">{{ t('governance.' + entry.label) }}</button>
          </nav>
          <div v-show="tab === 'overview'" data-test="site-overview-tab" class="min-w-0 space-y-5 p-4 sm:p-5">
            <SiteOverview :site="active" :snapshot="overviewSnapshot" :binding-count="bindings.filter(binding => binding.account_id > 0).length" :balance-health="balanceHealth" />
            <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900"><span class="font-medium">{{ t(!automation ? 'governance.automationUnknown' : automation.policy.enabled ? 'governance.automationOn' : 'governance.automationOff') }}</span><span class="min-w-0 flex-1 text-xs text-gray-500">{{ t('governance.automationSummaryHint') }}</span><button type="button" class="text-sm font-medium text-primary-700 dark:text-primary-300" @click="tab = 'monitor'">{{ t('governance.configureAutomation') }}</button></div>
            <ReconciliationPanel v-if="snapshot" :key="active.id" :site-id="active.id" :refresh-key="`${snapshot.id}:${reconciliationEpoch}`" :disabled="working" @busy="reconciliationBusy = $event" @applied="reconciled" />
            <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-900">{{ busy ? t('common.loading') : t('governance.noSnapshot') }}</p>
            <BalanceHealthPanel :health="balanceHealth" :disabled="working" @reload="reloadHealth" @configure="tab = 'monitor'" />
          </div>
          <div v-show="tab === 'import'" class="min-w-0 p-4 sm:p-5"><ImportPanel v-if="snapshot && importStateReady" :key="active.id" :site-id="active.id" :site-base-url="active.base_url" :site-platform="active.platform" :bindings="bindings" :managed-keys="managedKeys" :snapshot="snapshot" :groups="groups" :disabled="busy || balanceBusy || automationBusy || reconciliationBusy || keyBusy || rechargeBusy" @busy="importBusy = $event" @applied="reloadBindings" @manage-keys="openKeys" /><p v-else class="py-8 text-center text-sm text-gray-500">{{ busy ? t('common.loading') : t(snapshot ? 'governance.importStateUnavailable' : 'governance.noSnapshot') }}</p></div>
          <div v-show="tab === 'monitor'" class="min-w-0 space-y-5 p-4 sm:p-5"><AutomationPolicyPanel :key="active.id" :site-id="active.id" :configuration="automation" :disabled="working" @busy="automationBusy = $event" @saved="automationSaved" @reload="reloadAutomation" /><BalanceMonitorPanel :key="active.id" :site="active" :unit="snapshot?.catalog.account?.unit" :disabled="working" @saved="balanceSaved" @busy="balanceBusy = $event" /><RechargePlanPanel :key="active.id" :site-id="active.id" :disabled="working" @busy="rechargeBusy = $event" /><GovernanceHistory mode="bindings" :bindings="bindings" :groups="groups" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" :events="null" :checks="null" :disabled="working" @configure="configure" /></div>
          <div v-show="tab === 'history'" class="min-w-0 p-4 sm:p-5"><GovernanceHistory mode="history" :bindings="[]" :events="events" :checks="checks" :disabled="working" @acknowledge="acknowledge" @page="page" /></div>
        </div>
      </section>
      <BaseDialog v-if="keysOpen && active" :show="true" :title="t('governance.groupKeys')" width="wide" :show-close-button="!keyBusy" :close-on-escape="!keyBusy" @close="keysOpen = false"><ManagedKeysPanel :key="active.id" :site-id="active.id" :snapshot-id="snapshot?.id || 0" :groups="snapshot?.catalog.groups || []" :selections="keySelections" :disabled="busy || importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || rechargeBusy" @busy="keyBusy = $event" /></BaseDialog>
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
            ><label class="governance-checkbox-label flex items-center gap-2"
              ><input v-model="probe.probe_enabled" type="checkbox" class="governance-checkbox" />{{
                t('governance.enableMonitor')
              }}</label
            ><label class="block"
              >{{ t('governance.interval')
              }}<input
                v-model.number="probe.probe_interval_minutes"
                data-test="probe-interval"
                class="input w-full"
                type="number"
                min="1"
                step="1"
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
