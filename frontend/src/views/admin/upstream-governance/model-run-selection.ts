import type { ModelRun } from '@/api/admin/upstream-model-monitoring'

export function canDeleteModelRun(run: ModelRun): boolean {
  return run.request.config.target_type === 'local_group'
    && canSelectModelRun(run)
}

export function canSelectModelRun(run: ModelRun): boolean {
  return ['succeeded', 'failed', 'cancelled', 'indeterminate', 'skipped'].includes(run.status)
}
