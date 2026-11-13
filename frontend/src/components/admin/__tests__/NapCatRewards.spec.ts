import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import NapCatCampaignPanel from '../napcat/NapCatCampaignPanel.vue'
import NapCatInventoryPanel from '../napcat/NapCatInventoryPanel.vue'
import NapCatRecordsPanel from '../napcat/NapCatRecordsPanel.vue'
import { qqInviteAPI } from '@/api/qqInvite'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
vi.mock('@/api/admin/groups', () => ({ getAll: vi.fn(async () => [{ id: 7, name: 'Monthly', status: 'active', subscription_type: 'subscription', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: 100 }]) }))
vi.mock('@/api/qqInvite', () => ({ qqInviteAPI: { campaigns: vi.fn(), saveCampaign: vi.fn(), baseline: vi.fn(), codes: vi.fn(), previewCodes: vi.fn(), importCodes: vi.fn(), generateCodes: vi.fn(), records: vi.fn(), review: vi.fn(), recheck: vi.fn(), redeliver: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'admin@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const campaign = { id: 1, version: 2, name: 'November', enabled: false, group_ids: ['123456'], subscription_group_id: 7, starts_at: '2026-11-01T00:00:00Z', ends_at: '2026-12-01T00:00:00Z', welcome_template: 'Welcome {invitee}', congratulation_template: 'Thanks {inviter}', pending_template: 'Pending', delivery_pending_template: 'Please add the bot as a friend', private_template: '{code} {url}', baseline_ready: false, created_at: '2026-11-01T00:00:00Z' }
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear()
  auth.user = { id: 1, role: 'admin', email: 'admin@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(qqInviteAPI.campaigns).mockResolvedValue({ items: [{ ...campaign }], total: 1 })
  vi.mocked(qqInviteAPI.saveCampaign).mockResolvedValue({ ...campaign, version: 3 })
  vi.mocked(qqInviteAPI.codes).mockResolvedValue({ items: [], total: 0, available: 0, assigned: 0 })
  vi.mocked(qqInviteAPI.previewCodes).mockResolvedValue({ valid_count: 1, invalid_count: 0, items: [{ index: 0, valid: true, reason: '', code_hint: 'CODE****' }] })
  vi.mocked(qqInviteAPI.records).mockResolvedValue({ items: [{ id: 9, campaign_id: 1, group_id: '123456', invitee_qq: '234567', inviter_qq: '', status: 'pending_attribution', reason: 'untrusted_operator', created_at: '2026-11-01T00:00:00Z' }], total: 1 })
})
afterEach(() => wrapper?.unmount())
describe('NapCat campaigns', () => {
  it('requires a saved member baseline before enabling', async () => {
    wrapper = mount(NapCatCampaignPanel); await flushPromises()
    expect(wrapper.get('[data-test="campaign-toggle"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('qqInvite.baselineRequired')
    await wrapper.get('[data-test="baseline"]').trigger('click'); await flushPromises()
    expect(qqInviteAPI.baseline).toHaveBeenCalledWith(1, expect.any(AbortSignal))
  })
  it('blocks public redeem-code templates before sending an update', async () => {
    wrapper = mount(NapCatCampaignPanel); await flushPromises()
    await wrapper.get('[name="welcome_template"]').setValue('{code}')
    await wrapper.get('[data-test="campaign-form"]').trigger('submit'); await flushPromises()
    expect(qqInviteAPI.saveCampaign).not.toHaveBeenCalled(); expect(wrapper.text()).toContain('qqInvite.validation.groupCode')
  })
})
describe('NapCat stock', () => {
  it('requires preflight for exact current input and invalidates it after edits', async () => {
    wrapper = mount(NapCatInventoryPanel, { props: { campaignId: 1 } }); await flushPromises()
    await wrapper.get('textarea').setValue('PRIVATE-CODE')
    expect(wrapper.get('[data-test="import"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="preview"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="import"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('CODE****')
    await wrapper.get('textarea').setValue('DIFFERENT-CODE')
    expect(wrapper.get('[data-test="import"]').attributes('disabled')).toBeDefined(); expect(wrapper.text()).not.toContain('CODE****')
  })
  it('clears pasted private codes after session invalidation', async () => {
    wrapper = mount(NapCatInventoryPanel, { props: { campaignId: 1 } }); await flushPromises()
    await wrapper.get('textarea').setValue('PRIVATE-CODE'); auth.user.id = 2; await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
  })
})
describe('NapCat attribution and delivery', () => {
  it('shows revoked eligibility and prevents rechecking or resending its reward', async () => {
    vi.mocked(qqInviteAPI.records).mockResolvedValue({ items: [{ id: 9, campaign_id: 1, group_id: '123456', invitee_qq: '234567', inviter_qq: '345678', status: 'revoked', reason: 'invitee_left', reward_id: 10, private_status: 'sent', created_at: '2026-11-01T00:00:00Z' }], total: 1 })
    wrapper = mount(NapCatRecordsPanel, { props: { campaignId: 1 } }); await flushPromises()
    expect(wrapper.text()).toContain('qqInvite.recordStates.revoked')
    expect(wrapper.get('[data-test="redeliver"]').attributes('disabled')).toBeDefined()
    const recheck = wrapper.findAll('button').find(b => b.text() === 'qqInvite.recheck')!
    expect(recheck.attributes('disabled')).toBeDefined()
  })
  it('requires explicit inviter QQ plus reason and does not infer an approver as inviter', async () => {
    wrapper = mount(NapCatRecordsPanel, { props: { campaignId: 1 } }); await flushPromises()
    await wrapper.get('[data-test="review-open"]').trigger('click')
    expect((wrapper.get('[name="inviter_qq"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-test="review-form"]').trigger('submit'); await flushPromises()
    expect(qqInviteAPI.review).not.toHaveBeenCalled()
    await wrapper.get('[name="inviter_qq"]').setValue('345678'); await wrapper.get('[name="review_reason"]').setValue('Verified invite source')
    await wrapper.get('[data-test="review-form"]').trigger('submit'); await flushPromises()
    expect(qqInviteAPI.review).toHaveBeenCalledWith(9, '345678', 'Verified invite source', expect.any(AbortSignal))
  })
  it('requires confirmation to resend the original reward when delivery is unknown', async () => {
    vi.mocked(qqInviteAPI.records).mockResolvedValue({ items: [{ id: 9, campaign_id: 1, group_id: '123456', invitee_qq: '234567', inviter_qq: '345678', status: 'awarded', reason: '', reward_id: 10, private_status: 'unknown', created_at: '2026-11-01T00:00:00Z' }], total: 1 })
    wrapper = mount(NapCatRecordsPanel, { props: { campaignId: 1 } }); await flushPromises()
    expect(wrapper.get('[data-test="redeliver"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('qqInvite.unknownDeliveryHint')
    await wrapper.get('[data-test="redeliver"]').trigger('click')
    expect(qqInviteAPI.redeliver).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-redeliver"]').trigger('click'); await flushPromises()
    expect(qqInviteAPI.redeliver).toHaveBeenCalledWith(10, expect.any(AbortSignal))
  })
})
