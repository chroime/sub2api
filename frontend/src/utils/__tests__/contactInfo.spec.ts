import { describe, expect, it } from 'vitest'

import { parseContactInfo } from '../contactInfo'

describe('parseContactInfo', () => {
  it('turns configured lines into aligned label/value entries', () => {
    expect(parseContactInfo('QQ 群：421497493\n微 信：46656666')).toEqual([
      { label: 'QQ 群', value: '421497493' },
      { label: '微 信', value: '46656666' },
    ])
  })

  it('supports common separators and preserves unlabeled contact text', () => {
    expect(parseContactInfo('邮箱: support@example.com；客服在线')).toEqual([
      { label: '邮箱', value: 'support@example.com' },
      { label: '', value: '客服在线' },
    ])
  })

  it('ignores empty entries', () => {
    expect(parseContactInfo('  QQ：123  ||\n')).toEqual([{ label: 'QQ', value: '123' }])
  })
})
