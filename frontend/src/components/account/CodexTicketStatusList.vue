<template>
  <div class="space-y-1" data-testid="codex-ticket-status-list">
    <div
      v-for="ticket in tickets"
      :key="`${ticket.mode}:${ticket.model}`"
      :class="['flex flex-wrap items-center gap-x-2 gap-y-0.5', compact ? 'text-[10px] leading-4' : 'text-sm']"
      :data-ticket-mode="ticket.mode"
    >
      <span class="font-medium text-gray-600 dark:text-gray-300" :title="modeTitle(ticket.mode)">Codex {{ ticket.mode }}</span>
      <span class="truncate text-gray-500 dark:text-gray-400" :title="ticket.model">{{ compact ? shortModel(ticket.model) : ticket.model }}</span>
      <span v-if="ticket.length" class="text-gray-500 dark:text-gray-400" data-testid="codex-ticket-length" :title="t('admin.accounts.openai.codexTurnTicketLength')">{{ ticket.length }} B</span>
      <span v-if="ticket.ready" class="text-emerald-600 dark:text-emerald-400" :title="t('admin.accounts.openai.codexTurnTicketReady', { time: remaining(ticket.remaining_seconds) })">{{ remaining(ticket.remaining_seconds) }}</span>
      <span v-else-if="ticket.blocked" class="text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.codexTurnTicketPaused') }}</span>
      <span v-else class="text-gray-500">{{ t('admin.accounts.openai.codexTurnTicketMissing') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CodexTurnTicketStatus } from '@/types'

defineProps<{ tickets: CodexTurnTicketStatus[]; compact?: boolean }>()
const { t } = useI18n()
function modeTitle(mode: string) {
  if (mode === '292' || mode === '332') return t(`admin.accounts.openai.codexTicketMode${mode}`)
  return mode
}
function shortModel(model: string) {
  if (model === 'gpt-6-astra') return 'astra'
  if (model === 'gpt-5.6-sol') return 'sol'
  return model
}
function remaining(seconds: number) {
  const total = Math.max(0, Math.floor(seconds || 0))
  return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, '0')}s`
}
</script>
