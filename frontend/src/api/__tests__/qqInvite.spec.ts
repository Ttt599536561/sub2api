import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { qqInviteAPI } from '../qqInvite'
import { inviteError } from '@/utils/qqInvite'
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const adapter = apiClient.defaults.adapter
let requests: InternalAxiosRequestConfig[] = []
function login(id = 1, session = 'A') {
  localStorage.setItem('auth_user', JSON.stringify({ id }))
  localStorage.setItem('auth_token', 'fixture-token')
  localStorage.setItem('auth_session_id', session)
}
beforeEach(() => {
  localStorage.clear(); login(); requests = []
  apiClient.defaults.adapter = async config => { requests.push(config); return { data: { code: 0, data: {} }, status: 200, statusText: 'OK', headers: {}, config } }
})
afterEach(() => { apiClient.defaults.adapter = adapter })
describe('NapCat authenticated requests', () => {
  it('maps a campaign group lock through the real Axios error normalization and invitation request', async () => {
    const reason = 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED'
    apiClient.defaults.adapter = async config => {
      requests.push(config)
      throw new axios.AxiosError('Request failed with status code 409', 'ERR_BAD_REQUEST', config, undefined, {
        data: { code: 409, reason, message: 'PRIVATE SERVER DETAILS' },
        status: 409, statusText: 'Conflict', headers: {}, config
      })
    }
    const pending = qqInviteAPI.saveCampaign(1, {
      version: 2, name: 'Draft', enabled: false, group_ids: ['123456'], subscription_group_id: 8,
      starts_at: '2026-11-01T00:00:00Z', ends_at: '2026-12-01T00:00:00Z',
      welcome_template: 'Welcome {invitee}', congratulation_template: 'Thanks {inviter}',
      pending_template: 'Pending', delivery_pending_template: 'Please add the bot as a friend', private_template: '{code} {url}'
    })
    await expect(pending).rejects.toMatchObject({ status: 409, code: 409, reason })
    const error: unknown = await pending.catch(cause => cause)
    expect(inviteError(error)).toBe('campaignGroupLocked')
    expect(requests).toHaveLength(1)
    expect(requests[0]).toMatchObject({ method: 'put', url: '/admin/qq-invite/campaigns/1' })
  })
  const flows = [() => qqInviteAPI.config(), () => qqInviteAPI.connection(), () => qqInviteAPI.login('qrcode'), () => qqInviteAPI.login('refresh'), () => qqInviteAPI.login('restart'), () => qqInviteAPI.campaigns(), () => qqInviteAPI.baseline('1'), () => qqInviteAPI.codes('1', 1), () => qqInviteAPI.previewCodes('1', ['private']), () => qqInviteAPI.importCodes('1', ['private']), () => qqInviteAPI.generateCodes('1', 2), () => qqInviteAPI.records('1', '', 1), () => qqInviteAPI.review('1', '12345', 'verified'), () => qqInviteAPI.recheck('1'), () => qqInviteAPI.redeliver('1')]
  it.each(flows.map((start, i) => ({ start, i })))('rejects operation $i before dispatch after account switch', async ({ start }) => {
    const pending = start(); const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(2, 'B'); await rejected; expect(requests).toHaveLength(0)
  })
  it('posts explicit login and review actions without secrets in URLs', async () => {
    const signal = new AbortController().signal
    await qqInviteAPI.login('qrcode', signal); await qqInviteAPI.login('refresh', signal); await qqInviteAPI.login('restart', signal)
    await qqInviteAPI.review('a/b', '12345', 'verified with inviter', signal)
    expect(requests.map(r => r.url)).toEqual(['/admin/qq-invite/login/qrcode', '/admin/qq-invite/login/refresh', '/admin/qq-invite/login/restart', '/admin/qq-invite/records/a%2Fb/review'])
    expect(requests.every(r => r.signal === signal && r.method === 'post')).toBe(true)
    expect(JSON.parse(requests[3].data)).toEqual({ inviter_qq: '12345', reason: 'verified with inviter' })
  })
  it('rejects a late QR response after same-user relogin', async () => {
    let finish!: () => void, dispatched!: () => void
    const ready = new Promise<void>(resolve => { dispatched = resolve })
    apiClient.defaults.adapter = config => { dispatched(); return new Promise(resolve => { finish = () => resolve({ data: { url: 'private' }, status: 200, statusText: 'OK', headers: {}, config }) }) }
    const pending = qqInviteAPI.login('qrcode'); const rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await ready; login(1, 'B'); finish(); await rejected
  })
  it('uses campaign-scoped inventory and record routes with exact payloads', async () => {
    await qqInviteAPI.codes(12, 2)
    await qqInviteAPI.previewCodes(12, ['PRIVATE-CODE'])
    await qqInviteAPI.importCodes(12, ['PRIVATE-CODE'])
    await qqInviteAPI.generateCodes(12, 3)
    await qqInviteAPI.records(12, 'pending_attribution', 3)
    expect(requests.map(r => [r.method, r.url])).toEqual([
      ['get', '/admin/qq-invite/campaigns/12/codes'], ['post', '/admin/qq-invite/campaigns/12/codes/preview'],
      ['post', '/admin/qq-invite/campaigns/12/codes/import'], ['post', '/admin/qq-invite/campaigns/12/codes/generate'],
      ['get', '/admin/qq-invite/campaigns/12/records']
    ])
    expect(requests[0].params).toEqual({ page: 2, page_size: 20, timezone: expect.any(String) })
    expect(requests.slice(1, 4).map(r => JSON.parse(r.data))).toEqual([{ codes: ['PRIVATE-CODE'] }, { codes: ['PRIVATE-CODE'] }, { count: 3 }])
    expect(requests[4].params).toEqual({ status: 'pending_attribution', page: 3, page_size: 20, timezone: expect.any(String) })
  })
})
