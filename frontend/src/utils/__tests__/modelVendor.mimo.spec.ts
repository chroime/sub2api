import { describe, expect, it } from 'vitest'
import { isDomesticModelVendor, modelVendorLabel, modelVendorOf } from '../modelVendor'

describe('MiMo model pricing vendor', () => {
  it.each(['mimo-v2.6-pro', 'xiaomi/mimo-v2.6-pro', 'MiMo-V2.6-Pro'])(
    'classifies %s as MiMo rather than its OpenAI-compatible protocol', (name) => {
      const vendor = modelVendorOf(name, 'openai')
      expect(vendor).toBe('mimo')
      expect(isDomesticModelVendor(vendor)).toBe(true)
      expect(modelVendorLabel(vendor)).toBe('Xiaomi MiMo')
    }
  )
})
