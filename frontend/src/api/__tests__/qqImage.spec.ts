import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { qqImageAPI, adminQQImageAPI } from '../qqImage'
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
  apiClient.defaults.adapter = async config => { requests.push(config); return { data: { code: 0, data: { price: '0.100000' } }, status: 200, statusText: 'OK', headers: {}, config } }
})
afterEach(() => { apiClient.defaults.adapter = adapter })
const flows = [
  () => qqImageAPI.status(), () => qqImageAPI.request('abc'), () => qqImageAPI.authorize('abc'),
  () => qqImageAPI.sendEmailCode(), () => qqImageAPI.verifyEmail('123456'), () => qqImageAPI.revoke('1'),
  () => qqImageAPI.orders(), () => qqImageAPI.order('1'), () => qqImageAPI.image('1'), () => qqImageAPI.cancel('1'),
  () => adminQQImageAPI.config(), () => adminQQImageAPI.orders(), () => adminQQImageAPI.grants(),
  () => adminQQImageAPI.connection(),
  () => adminQQImageAPI.audit(), () => adminQQImageAPI.refund('1', 'approved'), () => adminQQImageAPI.reconcile('1'),
  () => adminQQImageAPI.discovery(), () => adminQQImageAPI.startDiscovery(), () => adminQQImageAPI.closeDiscovery('1'), () => adminQQImageAPI.confirmDiscovery('1'),
  () => adminQQImageAPI.revoke('1', 'approved'), () => adminQQImageAPI.image('1'), () => adminQQImageAPI.release('1', 'confirmed failure')
]
describe('QQ private requests', () => {
  it('reads connection status through an authenticated cancellable GET with no payload', async () => {
    const signal = new AbortController().signal
    await adminQQImageAPI.connection(signal)
    expect(requests[0].url).toBe('/admin/qq-image/connection')
    expect(requests[0].method).toBe('get')
    expect(requests[0].signal).toBe(signal)
    expect(requests[0].data).toBeUndefined()
    expect(requests[0].headers.get('Authorization')).toBe('Bearer fixture-token')
  })
  it.each(flows.map((start, i) => ({ start, i })))('rejects operation $i before dispatch after a login change', async ({ start }) => {
    const result = start(); const rejected = expect(result).rejects.toSatisfy(axios.isCancel)
    login(2, 'B'); await rejected; expect(requests).toHaveLength(0)
  })
  it('uses explicit discovery actions with encoded identifiers and no credential payload', async () => {
    const signal = new AbortController().signal
    await adminQQImageAPI.discovery(signal); await adminQQImageAPI.startDiscovery(signal)
    await adminQQImageAPI.closeDiscovery('a/b', signal); await adminQQImageAPI.confirmDiscovery('a/b', signal)
    expect(requests.map(request => [request.method, request.url])).toEqual([
      ['get', '/admin/qq-image/discovery'], ['post', '/admin/qq-image/discovery/start'],
      ['post', '/admin/qq-image/discovery/a%2Fb/close'], ['post', '/admin/qq-image/discovery/a%2Fb/confirm']
    ])
    expect(requests.every(request => request.signal === signal)).toBe(true)
    expect(requests.slice(1).map(request => JSON.parse(request.data))).toEqual([{}, {}, {}])
  })
  it('sends explicit consent and only the current email verification code', async () => {
    await qqImageAPI.authorize('a/b'); await qqImageAPI.verifyEmail('123456')
    expect(requests[0].url).toBe('/user/qq-image/requests/a%2Fb/authorize')
    expect(JSON.parse(requests[0].data)).toEqual({ consent: true })
    expect(JSON.parse(requests[1].data)).toEqual({ code: '123456' })
  })
  it('fetches images through authenticated binary requests', async () => {
    await qqImageAPI.image('order-1')
    expect(requests[0].responseType).toBe('blob')
    expect(requests[0].headers.get('Authorization')).toBe('Bearer fixture-token')
  })
  it.each([() => qqImageAPI.orders(), () => adminQQImageAPI.connection(), () => adminQQImageAPI.discovery(), () => adminQQImageAPI.startDiscovery(), () => adminQQImageAPI.confirmDiscovery('1')])('rejects late private data from a replaced session %#', async start => {
    let finish!: () => void, dispatched!: () => void
    const ready = new Promise<void>(resolve => { dispatched = resolve })
    apiClient.defaults.adapter = config => { dispatched(); return new Promise(resolve => { finish = () => resolve({ data: [], status: 200, statusText: 'OK', headers: {}, config }) }) }
    const pending = start(); const rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await ready; login(1, 'B'); finish(); await rejected
  })
})

// Reply text settings use a separate resource and the same session ownership guard.
describe('QQ reply template requests', () => {
  it('uses authenticated cancellable endpoints with exact revision payloads', async () => {
    const signal = new AbortController().signal
    const overrides = { 'common.menu': '菜单 {{rules}}' }
    await adminQQImageAPI.replyTemplates(signal)
    await adminQQImageAPI.saveReplyTemplates({ expected_revision: 3, overrides }, signal)
    await adminQQImageAPI.previewReplyTemplate({ key: 'common.menu', text: overrides['common.menu'] }, signal)
    await adminQQImageAPI.replyTemplateRevisions(signal)
    await adminQQImageAPI.replyTemplateRevision(2, signal)
    await adminQQImageAPI.rollbackReplyTemplates({ expected_revision: 3, revision: 2 }, signal)
    expect(requests.map(request => [request.method, request.url])).toEqual([
      ['get', '/admin/qq-image/reply-templates'], ['put', '/admin/qq-image/reply-templates'],
      ['post', '/admin/qq-image/reply-templates/preview'], ['get', '/admin/qq-image/reply-templates/revisions'],
      ['get', '/admin/qq-image/reply-templates/revisions/2'], ['post', '/admin/qq-image/reply-templates/rollback']
    ])
    expect(JSON.parse(requests[1].data)).toEqual({ expected_revision: 3, overrides })
    expect(JSON.parse(requests[2].data)).toEqual({ key: 'common.menu', text: overrides['common.menu'] })
    expect(JSON.parse(requests[5].data)).toEqual({ expected_revision: 3, revision: 2 })
    expect(requests.every(request => request.signal === signal && request.headers.get('Authorization') === 'Bearer fixture-token')).toBe(true)
  })
  it.each([
    () => adminQQImageAPI.replyTemplates(), () => adminQQImageAPI.saveReplyTemplates({ expected_revision: 0, overrides: {} }),
    () => adminQQImageAPI.previewReplyTemplate({ key: 'common.menu', text: 'example' }), () => adminQQImageAPI.replyTemplateRevisions(),
    () => adminQQImageAPI.replyTemplateRevision(1), () => adminQQImageAPI.rollbackReplyTemplates({ expected_revision: 1, revision: 0 })
  ])('cancels template operation %# before dispatch after a login change', async start => {
    const pending = start(); const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(2, 'B'); await rejected; expect(requests).toHaveLength(0)
  })
})
