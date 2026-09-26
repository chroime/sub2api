<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type BalanceMonitor,
  type Site,
} from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
import { errorKey } from './feedback'
const props = defineProps<{ site: Site; unit?: string; disabled?: boolean }>()
const emit = defineEmits<{ saved: [site: Site]; busy: [value: boolean] }>()
const { t } = useI18n()
const editing = ref(false),
  busy = ref(false),
  error = ref(''),
  saved = ref(false),
  recipients = ref('')
const form = ref<BalanceMonitor>({
  enabled: false,
  threshold: 10,
  unit: 'usd',
  recipients: [],
  cooldown_minutes: 1440,
})
let generation = 0
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
const state = computed(
  () =>
    props.site.balance_monitor_status?.state ||
    (props.site.balance_monitor?.enabled ? 'unknown' : 'disabled'),
)
const monitorError = computed(() => {
  const key = props.site.balance_monitor_status?.last_error || ''
  const keys: Record<string, string> = {
    balance_unavailable: 'balanceUnavailable',
    balance_unit_changed: 'balanceUnitChanged',
    email_unavailable: 'emailUnavailable',
    recipients_unavailable: 'recipientsUnavailable',
    email_delivery_failed: 'emailDeliveryFailed',
  }
  return key ? t('governance.' + (keys[key] || 'balanceMonitorError')) : ''
})
watch(
  () => [props.site.id, props.site.version],
  (current, previous) => {
    generation++
    form.value = props.site.balance_monitor
      ? {
          ...props.site.balance_monitor,
          recipients: [...props.site.balance_monitor.recipients],
        }
      : {
          enabled: false,
          threshold: 10,
          unit: props.site.platform === 'newapi' ? 'quota' : 'usd',
          recipients: [],
          cooldown_minutes: 1440,
        }
    recipients.value = form.value.recipients.join('\n')
    busy.value = false
    error.value = ''
    if (!previous || current[0] !== previous[0]) saved.value = false
  },
  { immediate: true },
)
onUnmounted(() => {
  generation++
  emit('busy', false)
})
async function save() {
  if (busy.value || props.disabled) return
  const request = generation
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    const result = await api.balanceMonitor(props.site.id, {
      ...form.value,
      version: props.site.version,
      recipients: [
        ...new Set(
          recipients.value
            .split(/[\s,;，；]+/)
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ],
    })
    if (request !== generation) return
    emit('saved', result)
    editing.value = false
    saved.value = true
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) busy.value = false
  }
}
</script>
<template>
  <section class="rounded-xl border border-gray-200 p-5 dark:border-dark-600">
    <div class="flex flex-wrap items-start gap-3">
      <div
        class="rounded-lg bg-primary-50 p-2 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
      >
        <Icon name="bell" size="md" />
      </div>
      <div class="min-w-0 flex-1">
        <h3 class="font-semibold">{{ t('governance.balanceMonitoring') }}</h3>
        <p class="mt-1 text-xs text-gray-500">
          {{ t('governance.balanceMonitorHint') }}
        </p>
      </div>
      <span
        class="rounded-full px-2.5 py-1 text-xs font-medium"
        :class="
          state === 'low'
            ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
            : state === 'healthy'
              ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
              : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
        "
        >{{ t(`governance.balanceState_${state}`) }}</span
      ><button
        id="governance-balance-settings"
        type="button"
        class="btn btn-secondary text-sm"
        :disabled="busy || disabled"
        :aria-expanded="editing"
        @click="editing = !editing"
      >
        {{ t(editing ? 'common.cancel' : 'governance.configureBalance') }}
      </button>
    </div>
    <div class="mt-4 grid gap-4 text-xs sm:grid-cols-3">
      <div>
        <p class="text-gray-500">{{ t('governance.balanceThreshold') }}</p>
        <p class="mt-1 font-medium tabular-nums">
          {{
            site.balance_monitor?.enabled
              ? '≤ ' +
                site.balance_monitor.threshold +
                ' ' +
                site.balance_monitor.unit.toUpperCase()
              : '—'
          }}
        </p>
      </div>
      <div>
        <p class="text-gray-500">{{ t('governance.lastEmailAttempt') }}</p>
        <p class="mt-1 tabular-nums">
          {{
            formatGovernanceTime(site.balance_monitor_status?.last_attempt_at)
          }}
        </p>
      </div>
      <div>
        <p class="text-gray-500">{{ t('governance.lastEmailSent') }}</p>
        <p class="mt-1 tabular-nums">
          {{
            formatGovernanceTime(site.balance_monitor_status?.last_notified_at)
          }}
        </p>
      </div>
    </div>
    <p
      v-if="site.balance_monitor_status?.last_error"
      role="alert"
      class="mt-3 text-xs text-red-600"
    >
      {{ monitorError }}
    </p>
    <p
      v-if="saved"
      role="status"
      class="mt-3 text-xs text-primary-700 dark:text-primary-300"
    >
      {{ t('governance.balanceMonitorSaved') }}
    </p>
    <form
      v-if="editing"
      class="mt-5 space-y-4 border-t border-gray-100 pt-5 dark:border-dark-700"
      @submit.prevent="save"
    >
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <fieldset :disabled="busy || disabled" class="min-w-0 space-y-4">
        <label class="flex items-center gap-2 text-sm font-medium"
          ><input
            v-model="form.enabled"
            data-test="balance-enabled"
            type="checkbox"
          />{{ t('governance.enableBalanceMonitor') }}</label
        >
        <div class="grid gap-4 sm:grid-cols-3">
          <label class="text-xs text-gray-500"
            >{{ t('governance.balanceThreshold')
            }}<input
              v-model.number="form.threshold"
              data-test="balance-threshold"
              type="number"
              min="0"
              step="any"
              required
              class="input mt-1 w-full" /></label
          ><label class="text-xs text-gray-500"
            >{{ t('governance.balanceUnit')
            }}<input
              :value="form.unit.toUpperCase()"
              readonly
              class="input mt-1 w-full bg-gray-50 dark:bg-dark-900" /></label
          ><label class="text-xs text-gray-500"
            >{{ t('governance.emailCooldown')
            }}<input
              v-model.number="form.cooldown_minutes"
              data-test="balance-cooldown"
              type="number"
              min="15"
              max="10080"
              required
              class="input mt-1 w-full"
          /></label>
        </div>
        <label class="block text-xs text-gray-500"
          >{{ t('governance.emailRecipients')
          }}<textarea
            v-model="recipients"
            data-test="balance-recipients"
            rows="2"
            class="input mt-1 w-full"
            :placeholder="t('governance.emailRecipientsPlaceholder')"
          />
        </label>
        <p class="text-xs text-gray-500">
          {{ t('governance.balanceRecipientsHint') }}
        </p>
        <div class="flex flex-wrap items-center gap-3">
          <button data-test="save-balance" class="btn btn-primary">
            {{ t('common.save') }}
          </button>
          <p class="max-w-xl text-xs text-gray-500">
            {{ t('governance.balanceSaveHint') }}
          </p>
        </div>
      </fieldset>
    </form>
  </section>
</template>
