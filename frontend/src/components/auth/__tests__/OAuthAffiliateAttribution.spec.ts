import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EmailOAuthButtons from '../EmailOAuthButtons.vue'
import LinuxDoOAuthSection from '../LinuxDoOAuthSection.vue'
import OidcOAuthSection from '../OidcOAuthSection.vue'
import DingTalkOAuthSection from '../DingTalkOAuthSection.vue'
import WechatOAuthSection from '../WechatOAuthSection.vue'
import { setAffiliateAttributionLocked } from '@/utils/oauthAffiliate'

vi.mock('vue-router', () => ({ useRoute: () => ({ query: { aff: 'URLCODE', aff_code: 'SECOND', redirect: '/dashboard' } }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ cachedPublicSettings: {}, publicSettingsLoaded: true }) }))
vi.mock('@/api/auth', () => ({ resolveWeChatOAuthStart: () => ({ mode: 'open', unavailableReason: null }) }))

describe('OAuth protected referral starts', () => {
  beforeEach(() => {
    sessionStorage.clear()
    localStorage.clear()
    setAffiliateAttributionLocked(true)
  })
  it.each([
    ['github', EmailOAuthButtons, { githubEnabled: true }],
    ['google', EmailOAuthButtons, { googleEnabled: true }],
    ['linuxdo', LinuxDoOAuthSection, {}],
    ['oidc', OidcOAuthSection, {}],
    ['dingtalk', DingTalkOAuthSection, {}],
    ['wechat', WechatOAuthSection, {}],
  ] as const)('omits legacy referral codes for %s while preserving the OAuth request', async (provider, component, props) => {
    const wrapper = mount(component as typeof LinuxDoOAuthSection, {
      props: { ...props, affCode: 'FORMCODE' },
      global: { stubs: { GitHubMark: true, GoogleMark: true } }
    })
    await wrapper.get('button').trigger('click')
    const event = wrapper.emitted('start')?.[0]?.[0] as { provider: string; params: Record<string, string> }
    expect(event.provider).toBe(provider)
    expect(event.params.redirect).toBe('/dashboard')
    expect(event.params).not.toHaveProperty('aff_code')
    expect(sessionStorage.getItem('oauth_aff_code')).toBeNull()
    expect(localStorage.getItem('affiliate_referral_code')).toBeNull()
  })
})
