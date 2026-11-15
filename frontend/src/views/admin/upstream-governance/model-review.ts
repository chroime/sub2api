import type { ModelRun } from '@/api/admin/upstream-model-monitoring'

// Automatic numerical assessment never writes or replaces a human proof review.
export function modelReviewState(run: ModelRun): string {
  if (run.review && run.review !== 'pending') return run.review
  if (run.request.template === 'candy' && run.status === 'succeeded' && run.result?.success && !run.result.error_code && ['numeric_correct', 'numeric_incorrect'].includes(run.result.candy_verdict)) {
    return `auto_${run.result.candy_verdict}`
  }
  return 'pending'
}
