import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { adminComplianceAPI } from '@/api/admin/compliance'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
let requests: InternalAxiosRequestConfig[]
const status = { required: false, version: 'version', document_path_zh: '', document_path_en: '', document_url_zh: '', document_url_en: '', ack_phrase_zh: '', ack_phrase_en: '' }
function login(id: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id, role: 'admin' }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${id}`)
}
beforeEach(() => {
  localStorage.clear(); login(7, 'session-A'); requests = []
  apiClient.defaults.adapter = async config => {
    requests.push(config)
    return { data: status, status: 200, statusText: 'OK', headers: {}, config }
  }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe('administrator acceptance request ownership', () => {
  it.each([8, 7])('never dispatches a queued acknowledgement after user %s starts another login', async id => {
    const pending = adminComplianceAPI.accept({ phrase: 'accepted by Alice', language: 'en' })
    const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(id, 'session-B')
    await rejected
    expect(requests).toHaveLength(0)
  })

  it('rejects an old successful reply after another login without adopting it', async () => {
    let finish!: (response: unknown) => void
    apiClient.defaults.adapter = config => {
      requests.push(config)
      return new Promise(resolve => { finish = value => resolve({ data: value, status: 200, statusText: 'OK', headers: {}, config }) })
    }
    const pending = adminComplianceAPI.accept({ phrase: 'accepted by Alice', language: 'en' })
    const rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await Promise.resolve(); await Promise.resolve()
    login(8, 'session-B'); finish(status)
    await rejected
    expect(requests[0].headers.get('Authorization')).toBe('Bearer token-7')
  })

  it('keeps normal acceptance endpoint, body and current-session success', async () => {
    const payload = { phrase: 'accepted by Alice', language: 'en' }
    expect(await adminComplianceAPI.accept(payload)).toEqual(status)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/compliance/accept')
    expect(JSON.parse(requests[0].data)).toEqual(payload)
    expect(requests[0].headers.get('Authorization')).toBe('Bearer token-7')
  })
})
