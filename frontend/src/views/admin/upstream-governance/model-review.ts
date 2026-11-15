import type { ModelRun } from '@/api/admin/upstream-model-monitoring'

// Automatic passes determine the final judgment without rewriting manual audit.
export function isAutomaticModelPass(run: ModelRun): boolean {
  return run.request.template === 'candy' && run.status === 'succeeded' && !!run.result?.success && !run.result.error_code && run.result.candy_verdict === 'numeric_correct'
}

export function modelReviewState(run: ModelRun): string {
  if (isAutomaticModelPass(run)) return 'pass'
  if (run.review && run.review !== 'pending') return run.review
  if (run.request.template === 'candy' && run.status === 'succeeded' && run.result?.success && !run.result.error_code && ['numeric_correct', 'numeric_incorrect'].includes(run.result.candy_verdict)) {
    return `auto_${run.result.candy_verdict}`
  }
  return 'pending'
}
