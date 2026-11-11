import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AlipayBillSettingsCard from '../AlipayBillSettingsCard.vue'
import Select from '@/components/common/Select.vue'
import type { ProviderInstance } from '@/types/payment'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post, put } }))
const provider: ProviderInstance = { id: 9, provider_key: 'alipay_bill', name: 'Built-in gateway', config: { appId: 'app-1', epayKeyConfigured: 'true', epayEnabled: 'true', epayNotifyOrigins: '["https://shop.example.com"]' }, supported_types: ['alipay'], enabled: false, payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, limits: '', sort_order: 0 }
const savedCredentials = { privateKey: '-----BEGIN PRIVATE KEY-----\nfixture-private-9\n-----END PRIVATE KEY-----', publicKey: '-----BEGIN PUBLIC KEY-----\nfixture-public-9\n-----END PUBLIC KEY-----' }

function mountCard(providers: ProviderInstance[] = []) {
  return mount(AlipayBillSettingsCard, { props: { providers, loading: false } })
}
function action(wrapper: ReturnType<typeof mountCard>, testId: string) { return wrapper.get(`[data-testid="${testId}"]`) }
function inputValue(wrapper: ReturnType<typeof mountCard>, id: string) { return (wrapper.get(id).element as HTMLInputElement).value }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail })
  return { promise, resolve, reject }
}

describe('standalone built-in EasyPay card', () => {
  beforeEach(() => { vi.resetAllMocks(); get.mockResolvedValue({ data: savedCredentials }); post.mockResolvedValue({ data: provider }); put.mockResolvedValue({ data: provider }) })

  it('saves a disabled three-credential draft independently and tests the saved record', async () => {
    const wrapper = mountCard()
    await wrapper.get('#bill-config-appId').setValue('app-1')
    await wrapper.get('#bill-config-privateKey').setValue('private-value')
    await wrapper.get('#bill-config-publicKey').setValue('public-value')
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/admin/payment/providers', expect.objectContaining({ provider_key: 'alipay_bill', enabled: false, payment_mode: 'qrcode', refund_enabled: false, config: expect.objectContaining({ appId: 'app-1', privateKey: 'private-value', publicKey: 'public-value', qrRules: '[]' }) }))
    expect(wrapper.emitted('saved')?.[0]?.[0]).toMatchObject({ id: 9, enabled: false })
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(savedCredentials.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(savedCredentials.publicKey)
    post.mockResolvedValue({ data: { success: true, checked_at: '2026-11-11T01:00:00Z', entry_count: 1, has_more: false } })
    await action(wrapper, 'bill-test').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith('/admin/payment/providers/9/bill-test')
    wrapper.unmount()
  })

  it('requires an explicit selection when multiple saved accounts exist', async () => {
    const wrapper = mountCard([provider, { ...provider, id: 10, name: 'Another account' }])
    expect(wrapper.find('#bill-config-appId').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.settings.payment.billSelectInstanceHint')
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '10')
    await flushPromises()
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/admin/payment/providers/10', expect.objectContaining({ enabled: false }))
    wrapper.unmount()
  })

  it('keeps other stored secrets masked and excludes unchanged fetched keys and the keyConfigured marker from updates', async () => {
    const wrapper = mountCard([provider])
    await flushPromises()
    expect((wrapper.get('#bill-epay-key').element as HTMLInputElement).value).toBe('')
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    const config = put.mock.calls[0][1].config
    expect(config).not.toHaveProperty('epayKey')
    expect(config).not.toHaveProperty('epayKeyConfigured')
    expect(config).not.toHaveProperty('privateKey')
    expect(config).not.toHaveProperty('publicKey')
    expect(config).not.toHaveProperty('appAuthToken')
    expect(config.epayNotifyOrigins).toBe('["https://shop.example.com"]')
    wrapper.unmount()
  })

  it('generates a key for copying only before save and clears its plaintext after saving', async () => {
    const wrapper = mountCard([provider])
    await action(wrapper, 'bill-generate-key').trigger('click')
    const generated = (wrapper.get('#bill-epay-key').element as HTMLInputElement).value
    expect(generated).toMatch(/^[a-f0-9]{64}$/)
    expect(action(wrapper, 'bill-copy-key').attributes('disabled')).toBeUndefined()
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(put.mock.calls[0][1].config.epayKey).toBe(generated)
    expect((wrapper.get('#bill-epay-key').element as HTMLInputElement).value).toBe('')
    expect(action(wrapper, 'bill-copy-key').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('rejects callback paths and public HTTP origins before saving', async () => {
    const wrapper = mountCard([provider])
    for (const origin of ['https://shop.example.com/notify', 'http://shop.example.com', 'https://shop.example.com?key=test']) {
      await wrapper.get('#bill-notify-origins').setValue(origin)
      await action(wrapper, 'bill-save').trigger('click')
      expect(wrapper.text()).toContain('admin.settings.payment.billOriginsInvalid')
      expect(put).not.toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('sends deletion to the existing confirmation flow with the chosen saved identity', async () => {
    const wrapper = mountCard([provider])
    await action(wrapper, 'bill-delete').trigger('click')
    expect(wrapper.emitted('delete')).toEqual([[provider]])
    expect(post).not.toHaveBeenCalled()
    expect(put).not.toHaveBeenCalled()
    await wrapper.setProps({ providers: [] })
    expect(wrapper.find('[data-testid="bill-delete"]').exists()).toBe(false)
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it.each([
    { enabled: true, receiverId: '', error: 'billReceiverRequired' },
    { enabled: false, receiverId: '2088bad', error: 'billReceiverInvalid' },
    { enabled: true, receiverId: '2088bad', error: 'billReceiverInvalid' },
    { enabled: true, receiverId: '2088123456789012', error: 'billRulesRequired' },
  ])('requires valid receiving identity and QR rules before collection: $enabled / $receiverId', async ({ enabled, receiverId, error }) => {
    const wrapper = mountCard([{ ...provider, enabled, config: { ...provider.config, receiverId } }])
    await action(wrapper, 'bill-save').trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toBe(`admin.settings.payment.${error}`)
    expect(put).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves exact QR amounts and forces QR-only, non-refundable capability when enabling collection', async () => {
    const rules = [{ id: 'ten', amount: '10.00', qr_url: 'https://qr.alipay.com/test', enabled: true, sort_order: 0 }]
    const wrapper = mountCard([{ ...provider, enabled: true, payment_mode: 'popup', refund_enabled: true, allow_user_refund: true, config: { ...provider.config, receiverId: '2088123456789012', qrRules: JSON.stringify(rules) } }])
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/admin/payment/providers/9', expect.objectContaining({ enabled: true, supported_types: ['alipay'], payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, config: expect.objectContaining({ qrRules: JSON.stringify(rules) }) }))
    wrapper.unmount()
  })

  it('waits for saved providers to finish loading before choosing an account or draft', async () => {
    const wrapper = mount(AlipayBillSettingsCard, { props: { providers: [], loading: true } })
    expect(wrapper.find('#bill-config-appId').exists()).toBe(false)
    await wrapper.setProps({ providers: [provider], loading: false })
    expect((wrapper.get('#bill-config-appId').element as HTMLInputElement).value).toBe('app-1')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('9')
    wrapper.unmount()
  })

  it('automatically displays saved application and Alipay keys using scoped labels without making the form dirty', async () => {
    get.mockResolvedValueOnce({ data: { ...savedCredentials, appAuthToken: 'unrequested-token-fixture', epayKey: 'unrequested-epay-fixture' } })
    const wrapper = mountCard([{ ...provider, config: { ...provider.config, appAuthToken: 'masked-token', epayKey: 'masked-key' } }])
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/admin/payment/providers/9/bill-credentials')
    expect(wrapper.get('label[for="bill-config-privateKey"]').text()).toContain('admin.settings.payment.billApplicationPrivateKey')
    expect(wrapper.get('label[for="bill-config-publicKey"]').text()).toContain('admin.settings.payment.billAlipayPublicKey')
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(savedCredentials.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(savedCredentials.publicKey)
    expect(inputValue(wrapper, '#bill-config-appAuthToken')).toBe('')
    expect(inputValue(wrapper, '#bill-epay-key')).toBe('')
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('refills the latest saved keys after save and reopening the card', async () => {
    const replacement = { ...savedCredentials, privateKey: 'replacement-private-fixture' }
    const wrapper = mountCard([provider])
    await flushPromises()
    await wrapper.get('#bill-config-privateKey').setValue(replacement.privateKey)
    get.mockResolvedValue({ data: replacement })
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/admin/payment/providers/9', expect.objectContaining({ config: expect.objectContaining({ privateKey: replacement.privateKey }) }))
    expect(get).toHaveBeenCalledTimes(2)
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(replacement.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(replacement.publicKey)
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
    const reopened = mountCard([provider])
    await flushPromises()
    expect(inputValue(reopened, '#bill-config-privateKey')).toBe(replacement.privateKey)
    reopened.unmount()
  })

  it('ignores a late credential response after selecting another saved account', async () => {
    const first = deferred<{ data: typeof savedCredentials }>()
    const second = deferred<{ data: typeof savedCredentials }>()
    get.mockImplementation((url: string) => url.includes('/9/') ? first.promise : second.promise)
    const wrapper = mountCard([provider, { ...provider, id: 10, name: 'Other account' }])
    expect(get).not.toHaveBeenCalled()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '9')
    await flushPromises()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '10')
    await flushPromises()
    const otherKeys = { privateKey: 'fixture-private-10', publicKey: 'fixture-public-10' }
    second.resolve({ data: otherKeys })
    await flushPromises()
    first.resolve({ data: savedCredentials })
    await flushPromises()
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(otherKeys.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(otherKeys.publicKey)
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('10')
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves edits made while saved credentials are loading and still fills the untouched key', async () => {
    const request = deferred<{ data: typeof savedCredentials }>()
    get.mockReturnValue(request.promise)
    const wrapper = mountCard([provider])
    await wrapper.get('#bill-config-privateKey').setValue('pending-private-fixture')
    await action(wrapper, 'bill-name').setValue('Pending account name')
    request.resolve({ data: savedCredentials })
    await flushPromises()
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe('pending-private-fixture')
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(savedCredentials.publicKey)
    expect(inputValue(wrapper, '[data-testid="bill-name"]')).toBe('Pending account name')
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ providers: [{ ...provider }] })
    expect(get).toHaveBeenCalledTimes(1)
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe('pending-private-fixture')
    wrapper.unmount()
  })

  it('reloads stored keys on a clean provider refresh without enabling a pending-save state', async () => {
    const wrapper = mountCard([provider])
    await flushPromises()
    const currentKeys = { privateKey: 'refreshed-private-fixture', publicKey: 'refreshed-public-fixture' }
    get.mockResolvedValueOnce({ data: currentKeys })
    await wrapper.setProps({ providers: [{ ...provider }] })
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(currentKeys.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(currentKeys.publicKey)
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves an explicitly cleared key through a provider refresh while its first read is pending', async () => {
    const request = deferred<{ data: typeof savedCredentials }>()
    get.mockReturnValue(request.promise)
    const wrapper = mountCard([provider])
    await wrapper.get('#bill-config-privateKey').setValue('')
    await wrapper.setProps({ providers: [{ ...provider }] })
    request.resolve({ data: savedCredentials })
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(1)
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe('')
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe(savedCredentials.publicKey)
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('resumes a pending credential read after a failed save and ignores the superseded response', async () => {
    const previousRead = deferred<{ data: typeof savedCredentials }>()
    get.mockReturnValueOnce(previousRead.promise)
    put.mockRejectedValueOnce(new Error('Save failed'))
    const wrapper = mountCard([provider])
    await action(wrapper, 'bill-name').setValue('Pending name')
    await action(wrapper, 'bill-save').trigger('click')
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(savedCredentials.privateKey)
    previousRead.resolve({ data: { privateKey: 'stale-private-fixture', publicKey: 'stale-public-fixture' } })
    await flushPromises()
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(savedCredentials.privateKey)
    expect(inputValue(wrapper, '[data-testid="bill-name"]')).toBe('Pending name')
    expect(wrapper.get('[role="alert"]').text()).toBe('Save failed')
    wrapper.unmount()
  })

  it('shows a credential read error and retries without erasing edits made before the retry', async () => {
    get.mockRejectedValueOnce(new Error('Read failed'))
    const wrapper = mountCard([provider])
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.payment.billCredentialsLoadFailed')
    await wrapper.get('#bill-config-publicKey').setValue('pending-public-fixture')
    await action(wrapper, 'bill-credentials-retry').trigger('click')
    await flushPromises()
    expect(inputValue(wrapper, '#bill-config-privateKey')).toBe(savedCredentials.privateKey)
    expect(inputValue(wrapper, '#bill-config-publicKey')).toBe('pending-public-fixture')
    expect(wrapper.text()).not.toContain('admin.settings.payment.billCredentialsLoadFailed')
    expect(action(wrapper, 'bill-test').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it.each(['draft', 'unmount'])('ignores a credential request that finishes after moving to %s', async (destination) => {
    const request = deferred<{ data: typeof savedCredentials }>()
    get.mockReturnValue(request.promise)
    const wrapper = mountCard([provider])
    const privateInput = wrapper.get('#bill-config-privateKey').element as HTMLTextAreaElement
    if (destination === 'unmount') wrapper.unmount()
    else await wrapper.findAll('button').find(button => button.text() === 'admin.settings.payment.billNewDraft')!.trigger('click')
    request.resolve({ data: savedCredentials })
    await flushPromises()
    if (destination === 'unmount') expect(privateInput.value).toBe('')
    else {
      expect(inputValue(wrapper, '#bill-config-privateKey')).toBe('')
      expect(inputValue(wrapper, '#bill-config-publicKey')).toBe('')
      expect(action(wrapper, 'bill-test').attributes('disabled')).toBeDefined()
      wrapper.unmount()
    }
  })
})
