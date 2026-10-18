import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountTableFilters from '../AccountTableFilters.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const filterNames = [
  'admin.accounts.allPlatforms',
  'admin.accounts.allTypes',
  'admin.accounts.allStatus',
  'admin.accounts.allPrivacyModes',
  'admin.accounts.allGroups',
]

describe('AccountTableFilters accessible names', () => {
  it.each([
    { platform: '', type: '', status: '', privacy_mode: '', group: '' },
    { platform: 'openai', type: 'oauth', status: 'active', privacy_mode: 'training_off', group: 'ungrouped' },
  ])('identifies every filter independently of its selected value: %j', (filters) => {
    const wrapper = mount(AccountTableFilters, {
      props: { searchQuery: '', filters },
      global: { stubs: { Icon: true } },
    })
    try {
      const buttons = wrapper.findAll('button.select-trigger')
      expect(buttons).toHaveLength(5)
      expect(buttons.map(button => button.attributes('aria-label'))).toEqual(filterNames)
    } finally {
      wrapper.unmount()
    }
  })
})
