import { CanceledError, type AxiosRequestTransformer } from 'axios'
import { apiClient, ownedAuthRequestConfig } from './client'
import { assertAuthSessionCurrent } from './authSession'

export type InviteID = string | number
export interface InviteConfig {
  version: number; enabled: boolean; onebot_url: string; webui_url: string; bot_qq: string; public_base_url: string
  onebot_token_configured: boolean; webui_token_configured: boolean; event_secret_configured: boolean; encryption_available: boolean
}
export type InviteConfigInput = Pick<InviteConfig, 'version' | 'enabled' | 'onebot_url' | 'webui_url' | 'bot_qq' | 'public_base_url'> & { onebot_token?: string; webui_token?: string; event_secret?: string }
export interface InviteConnection { online: boolean; offline?: boolean; core_ready: boolean; qr_login_accepted?: boolean; login_phase?: string; qq?: string; nickname?: string }
export interface InviteQR { url: string; restarting: boolean }
export interface InviteTemplates { welcome_template: string; congratulation_template: string; pending_template: string; delivery_pending_template: string; private_template: string }
export interface InviteCampaign extends InviteTemplates {
  id: InviteID; version: number; name: string; enabled: boolean; group_ids: string[]; subscription_group_id: number
  starts_at: string; ends_at: string; baseline_ready: boolean; created_at: string; plan_name?: string
}
export type InviteCampaignInput = Omit<InviteCampaign, 'id' | 'baseline_ready' | 'created_at' | 'plan_name'>
export interface InviteCode { id: InviteID; campaign_id: InviteID; code_hint: string; state: string; record_id?: InviteID; created_at: string }
export interface InviteCodePreview { valid_count: number; invalid_count: number; items: { index: number; valid: boolean; reason: string; code_hint: string }[] }
export interface InviteRecord {
  id: InviteID; campaign_id: InviteID; group_id: string; invitee_qq: string; inviter_qq: string; qq_level?: number
  status: string; reason: string; reward_id?: InviteID; code_hint?: string; private_status?: string; group_status?: string; created_at: string
}
export interface InvitePage<T> { items: T[]; total: number }
export interface InviteInventory extends InvitePage<InviteCode> { available: number; assigned: number }

// Bind every dispatch and response to the login that initiated it, including cross-tab changes.
async function request<T>(method: string, path: string, data?: unknown, signal?: AbortSignal, params?: Record<string, unknown>): Promise<T> {
  const owner = ownedAuthRequestConfig()
  let stored: string | null
  try { stored = localStorage.getItem('auth_session_id') } catch { throw new CanceledError('Authentication session unavailable') }
  function storageMatches() {
    try { return stored === localStorage.getItem('auth_session_id') && JSON.parse(localStorage.getItem('auth_user') || 'null')?.id === owner.authIdentity.userID } catch { return false }
  }
  const verify: AxiosRequestTransformer = body => {
    if (!storageMatches()) throw new CanceledError('Authentication session changed before dispatch')
    return body
  }
  const defaults = apiClient.defaults.transformRequest
  const response = await apiClient.request<T>({ ...owner, method, url: `/admin/qq-invite${path}`, data, signal, params,
    headers: { 'Cache-Control': 'no-store' },
    transformRequest: [verify, ...(Array.isArray(defaults) ? defaults : defaults ? [defaults] : [])] })
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  if (!storageMatches()) throw { code: 'AUTH_SESSION_CHANGED', status: 401 }
  return response.data
}
const id = (value: InviteID) => encodeURIComponent(String(value))
export const qqInviteAPI = {
  config: (signal?: AbortSignal) => request<InviteConfig>('get', '/config', undefined, signal),
  saveConfig: (data: InviteConfigInput, signal?: AbortSignal) => request<InviteConfig>('put', '/config', data, signal),
  connection: (signal?: AbortSignal) => request<InviteConnection>('get', '/connection', undefined, signal),
  login: (action: 'qrcode' | 'refresh' | 'restart', signal?: AbortSignal) => request<InviteQR>('post', `/login/${action}`, {}, signal),
  campaigns: (signal?: AbortSignal) => request<InvitePage<InviteCampaign>>('get', '/campaigns', undefined, signal),
  saveCampaign: (campaignID: InviteID | null, data: InviteCampaignInput, signal?: AbortSignal) => request<InviteCampaign>(campaignID ? 'put' : 'post', campaignID ? `/campaigns/${id(campaignID)}` : '/campaigns', data, signal),
  baseline: (campaignID: InviteID, signal?: AbortSignal) => request<unknown>('post', `/campaigns/${id(campaignID)}/baseline`, {}, signal),
  codes: (campaignID: InviteID, page: number, signal?: AbortSignal) => request<InviteInventory>('get', `/campaigns/${id(campaignID)}/codes`, undefined, signal, { page, page_size: 20 }),
  previewCodes: (campaignID: InviteID, codes: string[], signal?: AbortSignal) => request<InviteCodePreview>('post', `/campaigns/${id(campaignID)}/codes/preview`, { codes }, signal),
  importCodes: (campaignID: InviteID, codes: string[], signal?: AbortSignal) => request<InviteCodePreview>('post', `/campaigns/${id(campaignID)}/codes/import`, { codes }, signal),
  generateCodes: (campaignID: InviteID, count: number, signal?: AbortSignal) => request<InviteCodePreview>('post', `/campaigns/${id(campaignID)}/codes/generate`, { count }, signal),
  records: (campaignID: InviteID, status: string, page: number, signal?: AbortSignal) => request<InvitePage<InviteRecord>>('get', `/campaigns/${id(campaignID)}/records`, undefined, signal, { status, page, page_size: 20 }),
  review: (recordID: InviteID, inviter_qq: string, reason: string, signal?: AbortSignal) => request<unknown>('post', `/records/${id(recordID)}/review`, { inviter_qq, reason }, signal),
  recheck: (recordID: InviteID, signal?: AbortSignal) => request<unknown>('post', `/records/${id(recordID)}/recheck`, {}, signal),
  redeliver: (rewardID: InviteID, signal?: AbortSignal) => request<unknown>('post', `/rewards/${id(rewardID)}/redeliver`, {}, signal)
}
