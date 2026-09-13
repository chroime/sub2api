export interface HomeStoryLabels {
  captions: readonly string[]
  reducedMotion: string
}

export interface HomeStoryController {
  refresh: () => void
  dispose: () => void
}

const DURATION = 12000
const clamp = (value: number) => Math.max(0, Math.min(1, value))
const ease = (value: number) => value * value * (3 - 2 * value)

export function startHomeStoryMotion(root: HTMLElement, labels: () => HomeStoryLabels): HomeStoryController {
  const mascot = root.querySelector<SVGSVGElement>('#mascot-svg')
  const caption = root.querySelector<HTMLElement>('#story-caption')
  const pulse = root.querySelector<SVGCircleElement>('#flow-pulse')
  const track = root.querySelector<SVGPathElement>('#request-track')
  const modelTrack = root.querySelector<SVGPathElement>('#model-track')
  const bridgeBus = root.querySelector<SVGLineElement>('#bridge-bus')
  const bridgePulse = root.querySelector<SVGCircleElement>('#bridge-pulse')
  const response = root.querySelector<HTMLElement>('.response-signal')
  const providers = Array.from(root.querySelectorAll<HTMLElement>('[data-provider]'))
  const noop = () => {}
  mascot?.pauseAnimations?.()
  mascot?.setCurrentTime?.(0)
  if (!mascot || !caption || !pulse || !track || !modelTrack || !bridgeBus || !bridgePulse || !response ||
    typeof track.getTotalLength !== 'function' || typeof bridgeBus.getTotalLength !== 'function') {
    return { refresh: noop, dispose: noop }
  }

  const media = window.matchMedia('(prefers-reduced-motion: reduce)')
  const trackLength = track.getTotalLength()
  const bridgeLength = bridgeBus.getTotalLength()
  let elapsed = 0
  let timelineStart = performance.now()
  let frame: number | null = null
  let inView = true
  let disposed = false
  let activeProvider = -1

  function render(time: number) {
    if (disposed) return
    const reduced = media.matches
    const phase = time < 3000 ? 0 : time < 8500 ? 1 : 2
    const phaseStart = phase === 0 ? 0 : phase === 1 ? 3000 : 8500
    const phaseEnd = phase === 0 ? 3000 : phase === 1 ? 8500 : DURATION
    const progress = (time - phaseStart) / (phaseEnd - phaseStart)
    const fade = Math.min(clamp(progress * 6), clamp((1 - progress) * 6))
    mascot!.setCurrentTime?.(time / 1000)
    root.dataset.phase = String(phase)
    caption!.textContent = reduced ? labels().reducedMotion : labels().captions[phase]
    caption!.style.opacity = reduced ? '1' : String(.55 + .45 * fade)
    const nextProvider = phase === 1 && !reduced ? Math.min(7, Math.floor(progress * providers.length)) : -1
    if (activeProvider !== nextProvider) {
      providers.forEach((provider, index) => provider.classList.toggle('active', index === nextProvider))
      activeProvider = nextProvider
    }
    const routing = phase === 1 && !reduced
    root.classList.toggle('is-routing', routing)
    const bridgeProgress = routing ? ease(clamp(progress)) : 0
    const bridgePoint = bridgeBus!.getPointAtLength(bridgeLength * (.03 + .94 * bridgeProgress))
    bridgePulse!.setAttribute('cx', String(bridgePoint.x))
    bridgePulse!.setAttribute('cy', String(bridgePoint.y))
    bridgePulse!.setAttribute('opacity', String(routing ? fade : 0))
    const point = track!.getPointAtLength(trackLength * ease(phase === 2 ? 1 - progress : progress))
    pulse!.setAttribute('cx', String(point.x))
    pulse!.setAttribute('cy', String(point.y))
    pulse!.setAttribute('opacity', String(!reduced && phase !== 1 ? fade : 0))
    pulse!.style.fill = phase === 2 ? 'var(--response)' : 'var(--accent)'
    modelTrack!.style.opacity = String(routing ? fade * .7 : 0)
    modelTrack!.style.strokeDashoffset = String(-time / 65)
    response!.style.opacity = String(phase === 2 && !reduced ? Math.sin(progress * Math.PI) : 0)
  }

  const playing = () => !disposed && !media.matches && inView && !document.hidden
  function tick(now: number) {
    frame = null
    if (!playing()) return
    elapsed = (now - timelineStart) % DURATION
    render(elapsed)
    frame = requestAnimationFrame(tick)
  }
  function syncPlayback() {
    if (frame !== null) cancelAnimationFrame(frame)
    frame = null
    if (media.matches) elapsed = 0
    render(elapsed)
    if (playing()) {
      // Re-anchor one absolute clock; resuming must never multiply playback.
      timelineStart = performance.now() - elapsed
      frame = requestAnimationFrame(tick)
    }
  }
  const observer = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
    inView = entries[0]?.isIntersecting ?? true
    syncPlayback()
  }, { threshold: 0 }) : undefined
  observer?.observe(root)
  document.addEventListener('visibilitychange', syncPlayback)
  media.addEventListener?.('change', syncPlayback)
  syncPlayback()

  return {
    refresh: () => render(elapsed),
    dispose: () => {
      disposed = true
      if (frame !== null) cancelAnimationFrame(frame)
      frame = null
      observer?.disconnect()
      document.removeEventListener('visibilitychange', syncPlayback)
      media.removeEventListener?.('change', syncPlayback)
    },
  }
}
