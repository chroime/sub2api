// Keep interval values representable by the backend's integer-minute storage.
const maxStoredMinutes = 2_147_483_647

export function intervalValidationKey(value: unknown): string | null {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) {
    return 'governance.intervalPositiveInteger'
  }
  if (value > maxStoredMinutes) return 'governance.intervalTooLarge'
  return null
}
