import { describe, expect, it } from 'vitest'
import { canAutoIsolate, newPolicy, queueChecks, sampleTone, selectedAccountIds } from './quality'

describe('account quality safeguards', () => {
  it('starts policies disabled in record-and-notify mode with a model-scoped gate', () => {
    expect(newPolicy()).toMatchObject({ enabled: false, control_mode: 'record_notify', isolation_scope: 'model', interval_seconds: 600, failure_threshold: 2, recovery_threshold: 2 })
  })

  it('requires a validated adapter and matching account type before offering automatic isolation', () => {
    const capability = { platform: 'openai', api_mode: 'responses', account_types: ['apikey'], automatic_control: true, output_limit_mode: 'max_output_tokens', reason: '' }
    expect(canAutoIsolate([capability], 'openai', 'responses', 'apikey')).toBe(true)
    expect(canAutoIsolate([capability], 'openai', 'responses', 'oauth')).toBe(false)
    expect(canAutoIsolate([capability], 'openai', 'responses')).toBe(false)
    expect(canAutoIsolate([{ ...capability, automatic_control: false }], 'openai', 'responses', 'apikey')).toBe(false)
  })

  it('never colors a transport error or truncated output as a definite quality failure', () => {
    expect(sampleTone({ execution_status: 'completed', quality_verdict: 'fail' })).toBe('fail')
    expect(sampleTone({ execution_status: 'completed', quality_verdict: 'pass' })).toBe('pass')
    expect(sampleTone({ execution_status: 'timeout', quality_verdict: 'fail' })).toBe('error')
    expect(sampleTone({ execution_status: 'truncated', quality_verdict: 'fail' })).toBe('error')
    expect(sampleTone({ execution_status: 'completed', quality_verdict: 'needs_review' })).toBe('unknown')
  })

  it('validates and deduplicates explicit account IDs without silently accepting malformed targets', () => {
    expect(selectedAccountIds('12, 7\n12 9')).toEqual([12, 7, 9])
    expect(() => selectedAccountIds('12, 1oops')).toThrow()
    expect(() => selectedAccountIds('0')).toThrow()
  })

  it('bounds batch queue traffic and reports partial failures without dropping other accounts', async () => {
    let active = 0, maximum = 0
    const attempted: number[] = []
    const result = await queueChecks(Array.from({ length: 12 }, (_, index) => index), async id => {
      active++; maximum = Math.max(maximum, active); attempted.push(id)
      await Promise.resolve()
      active--
      if (id === 3) throw new Error('account is already running')
    })
    expect(maximum).toBeLessThanOrEqual(4)
    expect(new Set(attempted).size).toBe(12)
    expect(result).toEqual({ success: 11, failed: 1 })
  })
})
