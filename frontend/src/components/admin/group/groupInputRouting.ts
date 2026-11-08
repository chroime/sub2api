import type { GroupInputRoutingConfig } from '@/types'

export const inputRoutingLimits = { rules: 32, accounts: 128, groups: 32, tokens: 10_000_000 } as const

/** Clone arrays so editing, cancelling, and reopening never change the saved group. */
export function cloneGroupInputRouting(config?: GroupInputRoutingConfig | null): GroupInputRoutingConfig {
  return {
    enabled: config?.enabled ?? false,
    rules: (config?.rules ?? []).map(rule => ({
      ...rule,
      input_tokens_mode: rule.input_tokens_mode ?? 'range',
      min_input_tokens: rule.min_input_tokens ?? 0,
      account_ids: [...rule.account_ids],
      group_ids: [...rule.group_ids],
    })),
  }
}

export function inputRoutingRuleError(rule: GroupInputRoutingConfig['rules'][number]): string | null {
  if (rule.input_tokens_mode !== 'all' && (!Number.isInteger(rule.min_input_tokens) || rule.min_input_tokens < 0 || rule.min_input_tokens > inputRoutingLimits.tokens ||
    !Number.isInteger(rule.max_input_tokens) || rule.max_input_tokens < rule.min_input_tokens || rule.max_input_tokens > inputRoutingLimits.tokens)) {
    return 'rangeError'
  }
  if (!rule.account_ids.length && !rule.group_ids.length) return 'targetsError'
  return null
}
