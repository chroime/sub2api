<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import StatusIndicator from './StatusIndicator.vue'
import type { Binding, Transport } from '@/api/admin/upstream-governance'
// Imported accounts of one upstream. Event history lives in the operations timeline.
const props = defineProps<{
  bindings: Binding[]
  groups?: { id: number; name: string }[]
  remoteGroups?: { id: string; name: string }[]
}>()
const { t } = useI18n()
const remoteNames = computed(() => new Map(props.remoteGroups?.map(group => [group.id, group.name])))
</script>
<template>
  <section class="space-y-6">
    <section class="min-w-0 space-y-3">
      <h3 class="font-semibold">
        {{ t('governance.bindings') }}
        <span class="ml-1 text-sm font-normal text-gray-400">{{ bindings.length }}</span>
      </h3>
      <p v-if="!bindings.length" class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600">
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
              <StatusIndicator v-if="binding.account_deleted" class="ml-2 align-middle" :label="t('governance.accountDeleted')" tone="neutral" icon="xCircle" />
            </template>
            <template v-else>{{ t('governance.pendingImport') }}</template>
          </p>
          <p class="mt-1 text-xs text-gray-500" data-test="binding-targets">
            {{ t('governance.localGroup') }}：
            {{ (binding.local_group_ids?.length ? binding.local_group_ids : [binding.local_group_id]).map(id => groups?.find(group => group.id === id)?.name || '#' + id).join('、') }}
          </p>
        </div>
      </article>
    </section>
  </section>
</template>
