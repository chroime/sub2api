import { describe, expect, it } from 'vitest'
import { isDomesticModelVendor, modelVendorOf, resolveModelVendor } from '../modelVendor'

describe('Hunyuan abbreviated model IDs', () => {
  it.each(['hy3', 'hy4-preview', 'HY4', 'tencent/hy4-preview', 'hunyuan-turbo'])('classifies %s as a domestic Hunyuan model', (model) => {
    expect(modelVendorOf(model, 'openai')).toBe('hunyuan')
    expect(isDomesticModelVendor(modelVendorOf(model, 'openai'))).toBe(true)
  })

  it.each(['hybrid-model', 'my-hy4-model'])('does not guess a vendor for %s', (model) => {
    expect(resolveModelVendor(model)).toBeNull()
  })
})
