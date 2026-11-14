import type { ModelRun } from '@/api/admin/upstream-model-monitoring'

export function canDeleteModelRun(run: ModelRun): boolean {
  return run.request.config.target_type === 'local_group'
    && ['succeeded', 'failed', 'cancelled', 'indeterminate', 'skipped'].includes(run.status)
}
