import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TokenRankingView from '../TokenRankingView.vue'

describe('TokenRankingView', () => {
  it('renders the standalone token ranking surface without using the usage view', () => {
    const wrapper = mount(TokenRankingView, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          UserTokenRanking: { template: '<section data-testid="token-ranking-widget" />' },
        },
      },
    })

    expect(wrapper.find('[data-testid="token-ranking-widget"]').exists()).toBe(true)
  })
})
