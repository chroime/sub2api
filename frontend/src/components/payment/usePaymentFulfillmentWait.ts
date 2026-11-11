import { onScopeDispose, ref } from 'vue'

// Payment confirmation and account fulfillment are separate server states.
// Keep polling for a bounded period after payment, independently of QR expiry.
export function usePaymentFulfillmentWait(onTimeout: () => void) {
  const waiting = ref(false)
  const timedOut = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null

  function stop() {
    if (timer !== null) clearTimeout(timer)
    timer = null
  }

  function start() {
    if (waiting.value) return
    waiting.value = true
    timedOut.value = false
    timer = setTimeout(() => {
      timer = null
      timedOut.value = true
      onTimeout()
    }, 120_000)
  }

  function reset() {
    stop()
    waiting.value = false
    timedOut.value = false
  }

  onScopeDispose(stop)
  return { waiting, timedOut, start, stop, reset }
}
