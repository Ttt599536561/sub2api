import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import NapCatCampaignPanel from '../napcat/NapCatCampaignPanel.vue'
import { qqInviteAPI, type InviteCampaign } from '@/api/qqInvite'
import { getAll } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import zh from '@/i18n/locales/zh/qqInvite'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key.split('.').reduce<unknown>((value, part) =>
      value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, zh) ?? key,
    locale: { value: 'zh' }
  })
}))
vi.mock('@/api/admin/groups', () => ({ getAll: vi.fn() }))
vi.mock('@/api/qqInvite', () => ({ qqInviteAPI: { campaigns: vi.fn(), saveCampaign: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'admin@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

const campaign: InviteCampaign = {
  id: 1, version: 2, name: 'November', enabled: false, group_ids: ['123456'], subscription_group_id: 7,
  starts_at: '2026-11-01T00:00:00Z', ends_at: '2026-12-01T00:00:00Z', welcome_template: 'Welcome {invitee}',
  congratulation_template: 'Thanks {inviter}', pending_template: 'Pending', delivery_pending_template: 'Please add the bot as a friend',
  private_template: '{code} {url}', baseline_ready: true, created_at: '2026-11-01T00:00:00Z'
}
let wrapper: ReturnType<typeof mount>
const subscriptionGroup = () => wrapper.findAll('select')[1]
const nameInput = () => wrapper.get('[name="campaign_name"]')
async function save() {
  await wrapper.get('[data-test="campaign-form"]').trigger('submit')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  auth.user = { id: 1, role: 'admin', email: 'admin@example.test' }
  auth.sessionRevision = 'A'
  auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user))
  localStorage.setItem('auth_session_id', 'A')
  vi.mocked(getAll).mockResolvedValue([
    { id: 7, name: 'Monthly', status: 'active', subscription_type: 'subscription', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: 100 },
    { id: 8, name: 'New monthly', status: 'active', subscription_type: 'subscription', daily_limit_usd: 0, weekly_limit_usd: 0, monthly_limit_usd: 100 }
  ] as AdminGroup[])
  vi.mocked(qqInviteAPI.campaigns).mockResolvedValue({ items: [{ ...campaign }], total: 1 })
  vi.mocked(qqInviteAPI.saveCampaign).mockResolvedValue({ ...campaign, version: 3 })
})
afterEach(() => wrapper?.unmount())

describe('NapCat campaign editing', () => {
  it.each([
    { status: 'inactive' },
    { subscription_type: 'standard' },
    { monthly_limit_usd: null },
    { monthly_limit_usd: 0 },
    { monthly_limit_usd: -1 },
    { monthly_limit_usd: Number.NaN },
    { monthly_limit_usd: Number.POSITIVE_INFINITY },
    { monthly_limit_usd: 1e10 },
    { daily_limit_usd: -1 },
    { daily_limit_usd: Number.NaN },
    { daily_limit_usd: Number.POSITIVE_INFINITY },
    { daily_limit_usd: 1e10 },
    { weekly_limit_usd: -1 },
    { weekly_limit_usd: Number.NaN },
    { weekly_limit_usd: Number.POSITIVE_INFINITY },
    { weekly_limit_usd: 1e10 }
  ] as Partial<AdminGroup>[])('excludes a group that cannot back redeemable welfare passes: %j', async invalid => {
    vi.mocked(getAll).mockResolvedValue([
      { id: 7, name: 'Invalid monthly', status: 'active', subscription_type: 'subscription', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: 100, ...invalid }
    ] as AdminGroup[])
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    expect(subscriptionGroup().findAll('option')).toHaveLength(1)
    expect(wrapper.text()).toContain(zh.qqInvite.noGroups)
    await save()
    expect(qqInviteAPI.saveCampaign).not.toHaveBeenCalled()
  })

  it('allows positive monthly quota with optional or zero daily and weekly limits', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    expect(subscriptionGroup().findAll('option').map(option => option.attributes('value'))).toEqual(['0', '7', '8'])
  })

  it('explains the group editing boundary before submitting a draft group change with its version', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('仅从未启用、无月卡库存且无邀请历史的草稿活动可修改订阅分组；其他活动如需更换分组，请创建新活动。')
    await subscriptionGroup().setValue('8')
    await save()
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledTimes(1)
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledWith(1, expect.objectContaining({ version: 2, subscription_group_id: 8, enabled: false }), expect.any(AbortSignal))
  })

  it('shows the specific group lock without a connection or reload suggestion and preserves edits', async () => {
    vi.mocked(qqInviteAPI.saveCampaign).mockRejectedValueOnce({ status: 409, code: 409, reason: 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED', message: 'PRIVATE TOKEN' })
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    await subscriptionGroup().setValue('8')
    await nameInput().setValue('Unsaved draft')
    await save()
    expect(wrapper.get('[role="alert"]').text()).toBe('此活动已启用过，或已有月卡库存或邀请历史，不能修改订阅分组。如需更换分组，请创建新活动。')
    expect(wrapper.text()).not.toContain('PRIVATE TOKEN')
    expect((subscriptionGroup().element as HTMLSelectElement).value).toBe('8')
    expect((nameInput().element as HTMLInputElement).value).toBe('Unsaved draft')
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledTimes(1)
    expect(qqInviteAPI.campaigns).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain(zh.qqInvite.saved)
  })

  it('still presents stale versions as a reloadable conflict without retrying the mutation', async () => {
    vi.mocked(qqInviteAPI.saveCampaign).mockRejectedValueOnce({ status: 409, code: 409, reason: 'QQ_INVITE_CONFLICT' })
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    await nameInput().setValue('Unsaved draft')
    await save()
    expect(wrapper.get('[role="alert"]').text()).toBe(zh.qqInvite.conflict)
    expect((nameInput().element as HTMLInputElement).value).toBe('Unsaved draft')
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledTimes(1)
  })

  it('accepts the saved version even when further list reads are unavailable and uses it on the next save', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    vi.mocked(qqInviteAPI.campaigns).mockRejectedValue(new Error('List unavailable'))
    vi.mocked(qqInviteAPI.saveCampaign).mockResolvedValueOnce({ ...campaign, version: 3, name: 'Saved draft', subscription_group_id: 8 })
    await subscriptionGroup().setValue('8')
    await nameInput().setValue(' Saved draft ')
    await save()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(zh.qqInvite.saved)
    expect(wrapper.text()).toContain(`${zh.qqInvite.version} 3`)
    expect((nameInput().element as HTMLInputElement).value).toBe('Saved draft')
    expect((subscriptionGroup().element as HTMLSelectElement).value).toBe('8')
    await nameInput().setValue('Next edit')
    await save()
    expect(qqInviteAPI.saveCampaign).toHaveBeenNthCalledWith(2, 1, expect.objectContaining({ version: 3, name: 'Next edit', subscription_group_id: 8 }), expect.any(AbortSignal))
    expect(qqInviteAPI.campaigns).toHaveBeenCalledTimes(1)
  })

  it('selects a newly created campaign from the returned result for subsequent updates', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    await wrapper.get('[data-test="campaign-new"]').trigger('click')
    await nameInput().setValue('Created draft')
    await subscriptionGroup().setValue('8')
    await wrapper.get('[name="group_ids"]').setValue('123456')
    await wrapper.get('[name="starts_at"]').setValue('2026-11-01T00:00')
    await wrapper.get('[name="ends_at"]').setValue('2026-12-01T00:00')
    vi.mocked(qqInviteAPI.saveCampaign).mockResolvedValueOnce({ ...campaign, id: 2, version: 1, name: 'Created draft', subscription_group_id: 8 })
    vi.mocked(qqInviteAPI.campaigns).mockRejectedValue(new Error('List unavailable'))
    await save()
    expect(qqInviteAPI.saveCampaign).toHaveBeenNthCalledWith(1, null, expect.objectContaining({ version: 0, enabled: false }), expect.any(AbortSignal))
    expect((wrapper.findAll('select')[0].element as HTMLSelectElement).value).toBe('2')
    await save()
    expect(qqInviteAPI.saveCampaign).toHaveBeenNthCalledWith(2, 2, expect.objectContaining({ version: 1, name: 'Created draft', subscription_group_id: 8 }), expect.any(AbortSignal))
  })

  it('accepts the toggle result without a list read and uses the returned version for the next toggle', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    vi.mocked(qqInviteAPI.campaigns).mockRejectedValue(new Error('List unavailable'))
    vi.mocked(qqInviteAPI.saveCampaign).mockResolvedValueOnce({ ...campaign, version: 3, enabled: true })
    await wrapper.get('[data-test="campaign-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(zh.qqInvite.saved)
    expect(wrapper.get('[data-test="campaign-toggle"]').text()).toBe(zh.qqInvite.pauseCampaign)
    await wrapper.get('[data-test="campaign-toggle"]').trigger('click')
    await flushPromises()
    expect(qqInviteAPI.saveCampaign).toHaveBeenNthCalledWith(2, 1, expect.objectContaining({ version: 3, enabled: false }), expect.any(AbortSignal))
    expect(qqInviteAPI.campaigns).toHaveBeenCalledTimes(1)
  })

  it('preserves unsaved inputs if a toggle fails', async () => {
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    await nameInput().setValue('Unsaved draft')
    await subscriptionGroup().setValue('8')
    vi.mocked(qqInviteAPI.saveCampaign).mockRejectedValueOnce({ status: 409 })
    await wrapper.get('[data-test="campaign-toggle"]').trigger('click')
    await flushPromises()
    expect((nameInput().element as HTMLInputElement).value).toBe('Unsaved draft')
    expect((subscriptionGroup().element as HTMLSelectElement).value).toBe('8')
    expect(wrapper.get('[data-test="campaign-toggle"]').text()).toBe(zh.qqInvite.enableCampaign)
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledTimes(1)
  })

  it.each(['save', 'toggle'] as const)('discards a late %s result after same-user relogin', async action => {
    let resolve!: (value: InviteCampaign) => void
    vi.mocked(qqInviteAPI.saveCampaign).mockReturnValueOnce(new Promise(done => { resolve = done }))
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    if (action === 'save') await wrapper.get('[data-test="campaign-form"]').trigger('submit')
    else await wrapper.get('[data-test="campaign-toggle"]').trigger('click')
    expect(qqInviteAPI.saveCampaign).toHaveBeenCalledTimes(1)
    auth.sessionRevision = 'B'
    resolve({ ...campaign, version: 3, name: 'Late campaign' })
    await flushPromises()
    expect(wrapper.text()).toContain(zh.qqInvite.sessionChanged)
    expect(wrapper.find('[data-test="campaign-form"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Late campaign')
    expect(wrapper.text()).not.toContain(zh.qqInvite.saved)
    expect(qqInviteAPI.campaigns).toHaveBeenCalledTimes(1)
    expect(vi.mocked(qqInviteAPI.saveCampaign).mock.calls[0][2]?.aborted).toBe(true)
  })

  it('discards a saved response if another tab changed the login without a storage event', async () => {
    let resolve!: (value: InviteCampaign) => void
    vi.mocked(qqInviteAPI.saveCampaign).mockReturnValueOnce(new Promise(done => { resolve = done }))
    wrapper = mount(NapCatCampaignPanel)
    await flushPromises()
    await wrapper.get('[data-test="campaign-form"]').trigger('submit')
    localStorage.setItem('auth_session_id', 'B')
    resolve({ ...campaign, version: 3, name: 'Late campaign' })
    await flushPromises()
    expect(wrapper.text()).toContain(zh.qqInvite.sessionChanged)
    expect(wrapper.find('[data-test="campaign-form"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(zh.qqInvite.saved)
    expect(qqInviteAPI.campaigns).toHaveBeenCalledTimes(1)
  })
})
