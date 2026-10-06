import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import PublicHomeStoryB2 from '../PublicHomeStoryB2.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })

const providerAssets = ['openai', 'anthropic', 'google', 'xai', 'zai', 'moonshot', 'deepseek', 'minimax']
// Fixed LF-normalized fingerprints from approved B2 artwork, independent of preview output.
const approvedProviderHashes: Record<string, string> = {
  openai: '4496c57aab751b6025c646ec878b3a26970ea3b113e1e02fecfcb7cb730072a2',
  anthropic: '52854534a90616f66e00b6215d27be624d659570dda6cc4b4cdc99717cb49935',
  google: 'd9443b0123c308693ce3e07007f1d36a70d0f97fe7dfc9bbd10d5fd26d5012cb',
  xai: '8e12a5fa3cadfee840844290445a7b95b065e612be5f6a3f2cada79069aed7b4',
  zai: '3a9f4f4c4397d957dc5a5b5bd75d396eb1c15964816d63a2c3206d72d55d8531',
  moonshot: '2b64214c1abf084af1acc165530ce66f692306821bd22104c103b1b4610c3842',
  deepseek: '73199f91c00b2c15b214e648146c7238786a55dbb621819593c4262a5835763d',
  minimax: 'f1e02acee0c66f8fbc84cbfa62cfceeccbac3a38e12886e08e14f26bb4621d17',
}
const wrappers: VueWrapper[] = []
let now = 0
let hidden = false
let reduced = false
let nextFrame = 0
let frames: Map<number, FrameRequestCallback>
let mediaListeners: Set<EventListenerOrEventListenerObject>
let intersection: IntersectionObserverCallback
let disconnect: ReturnType<typeof vi.fn>
let setCurrentTime: ReturnType<typeof vi.fn>
let pauseAnimations: ReturnType<typeof vi.fn>
const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden')

function mountStory(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { en, zh } })
  const wrapper = mount(PublicHomeStoryB2, {
    props: { siteName: 'Configured site', detailsHref: '/model-plaza', ...props },
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  return { wrapper, i18n }
}

async function advance(time: number) {
  now = time
  const pending = [...frames.values()]
  frames.clear()
  pending.forEach(callback => callback(now))
  await nextTick()
}

async function changeMotion(value: boolean) {
  reduced = value
  const event = { matches: value } as MediaQueryListEvent
  mediaListeners.forEach(listener => {
    if (typeof listener === 'function') listener(event)
    else listener.handleEvent(event)
  })
  await nextTick()
}

beforeEach(() => {
  now = 0
  hidden = false
  reduced = false
  nextFrame = 0
  frames = new Map()
  mediaListeners = new Set()
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    frames.set(++nextFrame, callback)
    return nextFrame
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => frames.delete(id)))
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
    media: query,
    get matches() { return reduced },
    addEventListener: (_event: string, listener: EventListenerOrEventListenerObject) => mediaListeners.add(listener),
    removeEventListener: (_event: string, listener: EventListenerOrEventListenerObject) => mediaListeners.delete(listener),
  } as MediaQueryList))
  disconnect = vi.fn()
  vi.stubGlobal('IntersectionObserver', class {
    constructor(callback: IntersectionObserverCallback) { intersection = callback }
    observe = vi.fn()
    disconnect = disconnect
  })
  pauseAnimations = vi.fn()
  setCurrentTime = vi.fn()
  Object.defineProperty(SVGSVGElement.prototype, 'pauseAnimations', { configurable: true, value: pauseAnimations })
  Object.defineProperty(SVGSVGElement.prototype, 'setCurrentTime', { configurable: true, value: setCurrentTime })
  Object.defineProperty(SVGElement.prototype, 'getTotalLength', {
    configurable: true,
    value(this: SVGElement) { return this.id === 'request-track' ? 112 : 1020.8 },
  })
  Object.defineProperty(SVGElement.prototype, 'getPointAtLength', {
    configurable: true,
    value(this: SVGElement, distance: number) {
      return this.id === 'request-track' ? { x: 72 + distance, y: 180 } : { x: 69.6 + distance, y: 85 }
    },
  })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  for (const method of ['pauseAnimations', 'setCurrentTime']) Reflect.deleteProperty(SVGSVGElement.prototype, method)
  for (const method of ['getTotalLength', 'getPointAtLength']) Reflect.deleteProperty(SVGElement.prototype, method)
  if (hiddenDescriptor) Object.defineProperty(document, 'hidden', hiddenDescriptor)
  else Reflect.deleteProperty(document, 'hidden')
})

describe('PublicHomeStoryB2', () => {
  it('renders the B2 section with the approved real SVG and no legacy layout', () => {
    const { wrapper } = mountStory()
    expect(wrapper.element.tagName).toBe('SECTION')
    expect(wrapper.attributes('id')).toBe('story-scene')
    expect(wrapper.find('.hero > .hero-copy').exists()).toBe(true)
    const mascot = wrapper.get('.mascot-host > svg')
    expect(mascot.find('image').exists()).toBe(false)
    expect(mascot.findAll('animate, animateTransform').length).toBeGreaterThan(20)
    const approved = new DOMParser().parseFromString(readFileSync(resolve('public/xeno-alien-spin.svg'), 'utf8'), 'image/svg+xml')
    expect(mascot.attributes('viewBox')).toBe(approved.documentElement.getAttribute('viewBox'))
    expect(mascot.findAll('path').length).toBe(approved.querySelectorAll('path').length)
    expect(wrapper.find('.story-provider-coverage, .story-compatibility-gateway, .shell, .public-grid, button').exists()).toBe(false)
    expect(wrapper.findAll('.hero-title-line').map(line => line.text().replace(/\s+/g, ''))).toEqual(['1个API', '8+模型厂家', '128+前沿大模型'])
  })

  it('uses all eight original B2 provider assets and keeps link access configurable', async () => {
    const { wrapper } = mountStory()
    expect(wrapper.findAll('.provider').map(provider => provider.attributes('data-provider'))).toEqual(['OpenAI', 'Anthropic', 'Google', 'xAI', 'Z.ai', 'Moonshot', 'DeepSeek', 'MiniMax'])
    expect(wrapper.findAll('.provider img').map(img => img.attributes('src').match(/([^/]+)\.svg/)?.[1])).toEqual(providerAssets)
    for (const name of providerAssets) {
      const asset = readFileSync(resolve(`src/assets/home/providers/${name}.svg`), 'utf8').replace(/\r\n/g, '\n')
      expect(createHash('sha256').update(asset).digest('hex')).toBe(approvedProviderHashes[name])
    }
    expect(wrapper.findAll('.providers a')).toHaveLength(9)
    await wrapper.setProps({ detailsHref: '' })
    expect(wrapper.findAll('.provider')).toHaveLength(8)
    expect(wrapper.find('.providers a').exists()).toBe(false)
  })

  it('applies the shared B2 icon class to every scene glyph', () => {
    const { wrapper } = mountStory()
    const glyphs = wrapper.findAll('.hero-actions svg, .app-origin svg, .response-signal svg, .section-heading svg')
    expect(glyphs).toHaveLength(5)
    expect(glyphs.every(icon => icon.classes().includes('icon'))).toBe(true)
  })

  it('namespaces internal SVG definitions for separately mounted scenes', () => {
    const first = mountStory().wrapper
    const second = mountStory().wrapper
    const firstIds = new Set(first.findAll('.mascot-host svg [id]').map(node => node.attributes('id')))
    const secondIds = second.findAll('.mascot-host svg [id]').map(node => node.attributes('id'))
    expect(firstIds.size).toBeGreaterThan(5)
    expect(secondIds.every(id => !firstIds.has(id))).toBe(true)
    for (const path of first.findAll('.mascot-host [fill^="url(#"]')) {
      expect(firstIds.has(path.attributes('fill').slice(5, -1))).toBe(true)
    }
  })

  it('synchronizes request, routing and return geometry on one twelve-second clock', async () => {
    const { wrapper } = mountStory()
    expect(pauseAnimations).toHaveBeenCalledOnce()
    expect(frames.size).toBe(1)
    await advance(1500)
    expect(setCurrentTime).toHaveBeenLastCalledWith(1.5)
    expect(wrapper.get('#flow-pulse').attributes('cx')).toBe('128')
    expect(wrapper.get('#flow-pulse').attributes('cy')).toBe('180')
    expect(wrapper.get('#flow-pulse').attributes('opacity')).toBe('1')
    expect(wrapper.get('#model-track').element.getAttribute('style')).toContain('opacity: 0')
    await advance(5750)
    expect(wrapper.classes()).toContain('is-routing')
    expect(wrapper.get('#bridge-pulse').attributes('cx')).toBe('580')
    expect(wrapper.get('#bridge-pulse').attributes('cy')).toBe('85')
    expect(wrapper.get('#flow-pulse').attributes('opacity')).toBe('0')
    expect(wrapper.get('.provider.active').attributes('data-provider')).toBe('Z.ai')
    await advance(10250)
    expect(wrapper.attributes('data-phase')).toBe('2')
    expect(wrapper.get('#flow-pulse').attributes('cx')).toBe('128')
    expect(wrapper.get('#flow-pulse').attributes('style')).toContain('var(--response)')
    expect(wrapper.get('.response-signal').attributes('style')).toContain('opacity: 1')
    await advance(12000)
    expect(setCurrentTime).toHaveBeenLastCalledWith(0)
    expect(wrapper.get('#flow-pulse').attributes('cx')).toBe('72')
    expect(wrapper.attributes('data-phase')).toBe('0')
  })

  it('preserves one-times playback while hidden or outside the viewport', async () => {
    const { wrapper } = mountStory()
    await advance(1000)
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    expect(frames.size).toBe(0)
    now = 5000
    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(frames.size).toBe(1)
    await advance(6000)
    expect(setCurrentTime).toHaveBeenLastCalledWith(2)
    intersection([{ isIntersecting: false, target: wrapper.element } as unknown as IntersectionObserverEntry], {} as IntersectionObserver)
    expect(frames.size).toBe(0)
    now = 10000
    intersection([{ isIntersecting: true, target: wrapper.element } as unknown as IntersectionObserverEntry], {} as IntersectionObserver)
    await advance(11000)
    expect(setCurrentTime).toHaveBeenLastCalledWith(3)
  })

  it('renders a static reduced-motion state and cancels every lifecycle resource', async () => {
    const removeVisibility = vi.spyOn(document, 'removeEventListener')
    const { wrapper } = mountStory()
    await advance(5750)
    await changeMotion(true)
    expect(frames.size).toBe(0)
    expect(setCurrentTime).toHaveBeenLastCalledWith(0)
    expect(wrapper.get('#story-caption').text()).toBe('统一接入，按需调用。')
    expect(wrapper.find('.provider.active').exists()).toBe(false)
    expect(wrapper.get('#flow-pulse').attributes('opacity')).toBe('0')
    expect(wrapper.get('#bridge-pulse').attributes('opacity')).toBe('0')
    await changeMotion(false)
    expect(frames.size).toBe(1)
    wrapper.unmount()
    expect(frames.size).toBe(0)
    expect(mediaListeners.size).toBe(0)
    expect(disconnect).toHaveBeenCalledOnce()
    expect(removeVisibility).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
  })

  it('updates localized copy without restarting the running scene', async () => {
    const { wrapper, i18n } = mountStory()
    await advance(5750)
    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.get('#story-caption').text()).toBe('Routing to the right model.')
    expect(wrapper.get('#story-title').text()).toContain('model providers')
    await advance(6750)
    expect(setCurrentTime).toHaveBeenLastCalledWith(6.75)
  })
})
