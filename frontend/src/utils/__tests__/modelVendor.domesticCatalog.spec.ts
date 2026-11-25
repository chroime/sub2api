import { describe, expect, it } from 'vitest'
import { isDomesticModelVendor, modelVendorLabel, modelVendorOf } from '../modelVendor'

describe('domestic catalog vendors', () => {
  it.each([
    ['step-3.7-flash', 'step', 'StepFun'],
    ['baichuan-m2', 'baichuan', 'Baichuan'],
    ['baichuan-m3-plus', 'baichuan', 'Baichuan']
  ])('recognizes %s on an OpenAI-compatible channel', (model, vendor, label) => {
    expect(modelVendorOf(model, 'openai')).toBe(vendor)
    expect(isDomesticModelVendor(vendor)).toBe(true)
    expect(modelVendorLabel(vendor)).toBe(label)
  })
})
