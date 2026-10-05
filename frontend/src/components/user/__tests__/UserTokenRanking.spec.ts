import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import UserTokenRanking from '../UserTokenRanking.vue'

const getTokenRanking = vi.hoisted(() => vi.fn())

vi.mock('@/api', () => ({
  usageAPI: {
    getTokenRanking,
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => ({
      'usage.ranking.title': '总 Token 用量排行',
      'usage.ranking.subtitle': '按所选时间范围统计全部用户的总 Token 用量',
      'usage.ranking.periods.today': '今天',
      'usage.ranking.periods.yesterday': '昨天',
      'usage.ranking.periods.7d': '近 7 天',
      'usage.ranking.periods.30d': '近 30 天',
      'usage.ranking.total': '总 Token',
      'usage.ranking.user': '用户',
      'usage.ranking.requests': '请求数',
      'usage.ranking.empty': '暂无排行数据',
      'usage.ranking.loadFailed': '排行榜加载失败',
    }[key] ?? key),
  }),
}))

vi.mock('@/utils/format', () => ({
  formatCompactNumber: (value: number) => String(value),
}))

const ranking = {
  period: 'today',
  start_date: '2026-10-04',
  end_date: '2026-10-04',
  total_tokens: 123456,
  ranking: [
    { rank: 1, user_id: 1, email: 'a***@g***.com', requests: 10, input_tokens: 500, output_tokens: 600, cache_tokens: 0, total_tokens: 1100 },
    { rank: 2, user_id: 2, email: 'b***@g***.com', requests: 8, input_tokens: 400, output_tokens: 500, cache_tokens: 0, total_tokens: 900 },
    { rank: 3, user_id: 3, email: 'c***@g***.com', requests: 7, input_tokens: 300, output_tokens: 400, cache_tokens: 0, total_tokens: 700 },
    { rank: 4, user_id: 4, email: 'd***@g***.com', requests: 5, input_tokens: 200, output_tokens: 200, cache_tokens: 0, total_tokens: 400 },
  ],
  generated_at: '2026-10-04T12:00:00+08:00',
}

function mountRanking() {
  return mount(UserTokenRanking, {
    global: {
      stubs: {
        LoadingSpinner: { template: '<span />' },
      },
    },
  })
}

describe('UserTokenRanking', () => {
  it('loads today by default and renders masked users with medal ranks', async () => {
    getTokenRanking.mockResolvedValue(ranking)

    const wrapper = mountRanking()
    await flushPromises()

    expect(getTokenRanking).toHaveBeenCalledWith('today')
    expect(wrapper.text()).toContain('总 Token 用量排行')
    expect(wrapper.text()).toContain('a***@g***.com')
    expect(wrapper.text()).toContain('123456')
    expect(wrapper.find('[data-rank="1"]').classes()).toContain('rank-gold')
    expect(wrapper.find('[data-rank="2"]').classes()).toContain('rank-silver')
    expect(wrapper.find('[data-rank="3"]').classes()).toContain('rank-bronze')
  })

  it('reloads the selected period without changing the ranking endpoint', async () => {
    getTokenRanking.mockResolvedValue(ranking)

    const wrapper = mountRanking()
    await flushPromises()
    await wrapper.get('[data-period="7d"]').trigger('click')
    await flushPromises()

    expect(getTokenRanking).toHaveBeenLastCalledWith('7d')
  })
})
