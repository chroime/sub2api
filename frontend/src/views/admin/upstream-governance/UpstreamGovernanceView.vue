<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { errorKey, eventKeys, siteStateKeys } from './feedback'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImportPanel from './ImportPanel.vue'
import ConnectDialog from './ConnectDialog.vue'
import api, {
  type Site,
  type SiteInput,
  type Snapshot,
  type Binding,
  type Page,
  type GovernanceEvent,
  type Check,
} from '@/api/admin/upstream-governance'
import groupsAPI from '@/api/admin/groups'
import proxiesAPI from '@/api/admin/proxies'
import type { AdminGroup } from '@/types'
const { t } = useI18n()
const sites = ref<Site[]>([]),
  active = ref<Site | null>(null),
  snapshot = ref<Snapshot | null>(null),
  bindings = ref<Binding[]>([])
const groups = ref<AdminGroup[]>([]),
  proxies = ref<{ id: number; name: string }[]>([])
const events = ref<Page<GovernanceEvent> | null>(null),
  checks = ref<Page<Check> | null>(null)
const busy = ref(false),
  error = ref(''),
  connecting = ref(false),
  editing = ref(false),
  deleting = ref(false),
  editId = ref<number | null>(null),
  editVersion = ref(0)
const form = ref<SiteInput>({
  name: '',
  platform: 'sub2api',
  base_url: '',
  proxy_id: null,
  enabled: true,
  interval_minutes: 15,
})
const probe = ref<Binding | null>(null),
  probeAction = ref<'check' | 'monitor'>('check')
function failure(e: unknown) {
  error.value = t(errorKey(e))
}
async function run(action: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await action()
  } catch (e) {
    failure(e)
  } finally {
    busy.value = false
  }
}
async function load() {
  await run(async () => {
    const data = await api.list()
    sites.value = data
    if (active.value)
      active.value = data.find((s) => s.id === active.value?.id) || null
  })
}
async function select(site: Site) {
  active.value = site
  snapshot.value = null
  events.value = null
  checks.value = null
  bindings.value = []
  busy.value = true
  error.value = ''
  const results = await Promise.allSettled([
    api.catalog(site.id),
    api.bindings(site.id),
    api.events(site.id),
    api.checks(site.id),
  ])
  if (active.value?.id === site.id) {
    if (results[0].status === 'fulfilled') snapshot.value = results[0].value
    else if ((results[0].reason as { status?: number }).status !== 404)
      failure(results[0].reason)
    if (results[1].status === 'fulfilled') bindings.value = results[1].value
    else failure(results[1].reason)
    if (results[2].status === 'fulfilled') events.value = results[2].value
    else failure(results[2].reason)
    if (results[3].status === 'fulfilled') checks.value = results[3].value
    else failure(results[3].reason)
  }
  busy.value = false
}
function edit(site?: Site) {
  editId.value = site?.id ?? null
  editVersion.value = site?.version ?? 0
  form.value = site
    ? {
        name: site.name,
        platform: site.platform,
        base_url: site.base_url,
        proxy_id: site.proxy_id,
        enabled: site.enabled,
        interval_minutes: site.interval_minutes,
      }
    : {
        name: '',
        platform: 'sub2api',
        base_url: '',
        proxy_id: null,
        enabled: true,
        interval_minutes: 15,
      }
  editing.value = true
}
async function save() {
  await run(async () => {
    const saved = editId.value
      ? await api.update(editId.value, {
          ...form.value,
          version: editVersion.value,
        })
      : await api.create(form.value)
    editing.value = false
    sites.value = await api.list()
    await select(saved)
  })
}
async function sync() {
  if (!active.value) return
  const id = active.value.id
  await run(async () => {
    snapshot.value = await api.sync(id)
    sites.value = await api.list()
    active.value = sites.value.find((s) => s.id === id) || null
    events.value = await api.events(id)
  })
}
async function remove() {
  if (!active.value) return
  await run(async () => {
    await api.remove(active.value!.id)
    active.value = null
    deleting.value = false
    sites.value = await api.list()
  })
}
async function connected() {
  connecting.value = false
  const id = active.value?.id
  await load()
  if (id && active.value) await select(active.value)
}
async function page(kind: 'events' | 'checks', n: number) {
  if (!active.value) return
  await run(async () => {
    if (kind === 'events') events.value = await api.events(active.value!.id, n)
    else checks.value = await api.checks(active.value!.id, n)
  })
}
async function acknowledge(id: number) {
  await run(async () => {
    await api.acknowledge(active.value!.id, id)
    events.value = await api.events(active.value!.id, events.value?.page)
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
  await run(async () => {
    const b = probe.value!
    if (probeAction.value === 'check') {
      await api.check(active.value!.id, b.id, b.probe_model)
      checks.value = await api.checks(active.value!.id)
    } else {
      await api.monitor(active.value!.id, b.id, {
        enabled: b.probe_enabled,
        model: b.probe_model,
        interval_minutes: b.probe_interval_minutes,
      })
      bindings.value = await api.bindings(active.value!.id)
    }
    probe.value = null
  })
}
onMounted(async () => {
  await load()
  await run(async () => {
    const [g, p] = await Promise.all([groupsAPI.getAll(), proxiesAPI.getAll()])
    groups.value = g
    proxies.value = p.map(({ id, name }) => ({ id, name }))
  })
})
</script>
<template>
  <AppLayout
    ><div class="space-y-6 text-gray-900 dark:text-gray-100">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-xl font-semibold">{{ t('governance.title') }}</h2>
          <p class="mt-2 max-w-3xl text-sm text-gray-500">
            {{ t('governance.description') }}
          </p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="busy" @click="load">
            {{ t('common.refresh') }}</button
          ><button class="btn btn-primary" :disabled="busy" @click="edit()">
            {{ t('governance.add') }}
          </button>
        </div>
      </header>
      <p
        v-if="error"
        role="alert"
        class="rounded border border-red-300 p-3 text-red-600"
      >
        {{ error }}
      </p>
      <p v-if="busy" role="status">{{ t('common.loading') }}</p>
      <p
        v-if="!busy && !sites.length"
        class="rounded-xl border border-dashed p-8 text-center"
      >
        {{ t('governance.empty') }}
      </p>
      <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <button
          v-for="site in sites"
          :key="site.id"
          class="rounded-xl border p-4 text-left dark:border-dark-600"
          :class="active?.id === site.id ? 'ring-2 ring-primary-500' : ''"
          :disabled="busy"
          @click="select(site)"
        >
          <strong>{{ site.name }}</strong>
          <p class="break-all text-sm">
            {{ site.platform }} · {{ site.base_url }}
          </p>
          <p class="text-sm">
            {{
              site.enabled ? t('governance.autoOn') : t('governance.autoOff')
            }}
            · {{ site.interval_minutes }} {{ t('governance.minutes') }}
          </p>
          <p class="text-sm">
            {{ t('governance.' + (siteStateKeys[site.status] || 'unknown')) }} · {{ site.last_sync_at || t('governance.never') }}
          </p>
          <p v-if="site.last_error" class="text-sm text-red-600">
            {{ t(errorKey({ reason: site.last_error })) }}
          </p>
        </button>
      </div>
      <template v-if="active">
        <section class="flex flex-wrap items-center gap-2">
          <h3 class="mr-auto text-lg font-semibold">{{ active.name }}</h3>
          <button
            class="btn btn-secondary"
            :disabled="busy"
            @click="edit(active)"
          >
            {{ t('common.edit') }}</button
          ><button
            class="btn btn-secondary"
            :disabled="busy"
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
            class="btn btn-primary"
            :disabled="busy || !active.has_credential"
            @click="sync"
          >
            {{ t('governance.sync') }}</button
          ><button
            class="btn btn-secondary"
            :disabled="busy"
            @click="deleting = true"
          >
            {{ t('common.delete') }}
          </button>
        </section>
        <p
          v-if="active.status === 'reauth_required'"
          role="alert"
          class="text-amber-700"
        >
          {{ t('governance.reauth') }}
        </p>
        <ImportPanel
          v-if="snapshot"
          :key="active.id"
          :site-id="active.id"
          :snapshot="snapshot"
          :groups="groups"
          :disabled="busy"
          @busy="busy = $event"
          @applied="
            run(async () => {
              bindings = await api.bindings(active!.id)
            })
          "
        />
        <p v-else-if="!busy">{{ t('governance.noSnapshot') }}</p>
        <section class="space-y-3">
          <h3 class="text-lg font-semibold">{{ t('governance.bindings') }}</h3>
          <p class="text-sm text-gray-500">{{ t('governance.billable') }}</p>
          <p v-if="!bindings.length">{{ t('governance.noBindings') }}</p>
          <article
            v-for="binding in bindings"
            :key="binding.id"
            class="flex flex-wrap items-center gap-3 rounded border p-3 dark:border-dark-600"
          >
            <span class="mr-auto"
              >{{ binding.remote_group_id }} / {{ binding.platform }} → {{ binding.account_id > 0 ? '#' + binding.account_id : t('governance.pendingImport') }}
              ·
              {{
                binding.probe_enabled
                  ? t('governance.monitorOn')
                  : t('governance.monitorOff')
              }}
              · {{ binding.probe_model }}</span
            ><button
              class="btn btn-secondary"
              :disabled="busy || binding.account_id <= 0"
              @click="configure(binding, 'check')"
            >
              {{ t('governance.check') }}</button
            ><button
              class="btn btn-secondary"
              :disabled="busy || binding.account_id <= 0"
              @click="configure(binding, 'monitor')"
            >
              {{ t('governance.monitor') }}
            </button>
          </article>
        </section>
        <section v-if="events" class="space-y-3">
          <h3 class="text-lg font-semibold">
            {{ t('governance.events') }} ({{ events.total }})
          </h3>
          <p v-if="!events.items.length">{{ t('governance.noEvents') }}</p>
          <article
            v-for="event in events.items"
            :key="event.id"
            class="rounded border p-3 dark:border-dark-600"
          >
            <p>
              {{
                t('governance.' + (eventKeys[event.kind] || 'eventChange'))
              }}
              · {{ event.resource }} · {{ event.created_at }}
            </p>
            <p class="break-words">
              {{ t('governance.before') }}: {{ event.before }}
            </p>
            <p class="break-words">
              {{ t('governance.after') }}: {{ event.after }}
            </p>
            <button
              class="btn btn-secondary mt-2"
              :disabled="busy || event.acknowledged"
              @click="acknowledge(event.id)"
            >
              {{
                t(
                  event.acknowledged
                    ? 'governance.acknowledged'
                    : 'governance.ack',
                )
              }}
            </button>
          </article>
          <div class="flex gap-3">
            <button
              class="btn btn-secondary"
              :disabled="busy || events.page <= 1"
              @click="page('events', events.page - 1)"
            >
              {{ t('governance.previous') }}</button
            ><span>{{ events.page }} / {{ Math.max(1, events.pages) }}</span
            ><button
              class="btn btn-secondary"
              :disabled="busy || events.page >= events.pages"
              @click="page('events', events.page + 1)"
            >
              {{ t('governance.next') }}
            </button>
          </div>
        </section>
        <section v-if="checks" class="space-y-3">
          <h3 class="text-lg font-semibold">
            {{ t('governance.checks') }} ({{ checks.total }})
          </h3>
          <p v-if="!checks.items.length">{{ t('governance.noChecks') }}</p>
          <p v-for="check in checks.items" :key="check.id">
            #{{ check.binding_id }} · {{ check.model }} ·
            {{
              t(check.success ? 'governance.success' : 'governance.failed')
            }}
            · {{ check.latency_ms }} ms · {{ check.error_code }} ·
            {{ check.created_at }}
          </p>
          <div class="flex gap-3">
            <button
              class="btn btn-secondary"
              :disabled="busy || checks.page <= 1"
              @click="page('checks', checks.page - 1)"
            >
              {{ t('governance.previous') }}</button
            ><span>{{ checks.page }} / {{ Math.max(1, checks.pages) }}</span
            ><button
              class="btn btn-secondary"
              :disabled="busy || checks.page >= checks.pages"
              @click="page('checks', checks.page + 1)"
            >
              {{ t('governance.next') }}
            </button>
          </div>
        </section>
      </template>
      <ConnectDialog
        v-if="connecting && active"
        :site-id="active.id"
        @close="connecting = false"
        @connected="connected"
      />
      <BaseDialog
        :show="editing"
        :title="t(editId ? 'governance.edit' : 'governance.add')"
        :show-close-button="!busy"
        :close-on-escape="!busy"
        @close="editing = false"
        ><form class="space-y-4" @submit.prevent="save">
          <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
          <label class="block"
            >{{ t('governance.name')
            }}<input
              v-model="form.name"
              class="input w-full"
              required
              maxlength="100" /></label
          ><label class="block"
            >{{ t('governance.platform')
            }}<select v-model="form.platform" class="input w-full">
              <option value="sub2api">Sub2API</option>
              <option value="newapi">New API</option>
            </select></label
          ><label class="block"
            >{{ t('governance.url')
            }}<input
              v-model="form.base_url"
              class="input w-full"
              type="url"
              placeholder="https://upstream.example"
              required
          /></label>
          <p class="text-sm text-gray-500">{{ t('governance.urlHint') }}</p>
          <label class="block"
            >{{ t('governance.proxy')
            }}<select v-model="form.proxy_id" class="input w-full">
              <option :value="null">{{ t('governance.direct') }}</option>
              <option
                v-for="proxy in proxies"
                :key="proxy.id"
                :value="proxy.id"
              >
                {{ proxy.name }}
              </option>
            </select></label
          ><label class="flex gap-2"
            ><input v-model="form.enabled" type="checkbox" />{{
              t('governance.autoOn')
            }}</label
          ><label class="block"
            >{{ t('governance.interval')
            }}<input
              v-model.number="form.interval_minutes"
              class="input w-full"
              type="number"
              min="5"
              max="1440"
              required /></label
          ><button class="btn btn-primary" :disabled="busy">
            {{ t('common.save') }}
          </button>
        </form></BaseDialog
      >
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
