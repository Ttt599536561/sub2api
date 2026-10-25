import { CanceledError, type AxiosRequestConfig, type AxiosRequestTransformer } from 'axios'
import { apiClient, ownedAuthRequestConfig } from './client'
import { assertAuthSessionCurrent } from './authSession'

export interface QQGrant {
  id: string; group_id: string; subject_hint?: string; status: string; created_at: string; user_id?: number
}
export interface QQStatus { enabled: boolean; email_verified: boolean; recent_authorization: boolean; email: string; grants: QQGrant[] }
export interface QQBindingRequest { id: string; status: string; group_id: string; subject_hint: string; expires_at: string }
export interface QQConfirmation { confirmation_code: string; expires_at: string }
export interface QQOrder {
  id: string; prompt: string; price: string; model: string; size: string; status: string; funds_status: string
  delivery_status: string; created_at: string; deadline: string; artifact_expires_at?: string; error?: string; user_id?: number
  currency?: 'USD'; provider_cost?: string | null; provider_currency?: string | null; provider_cost_status?: 'unknown'
}
export type QQReceiveMode = 'webhook' | 'websocket'
export interface QQConnection {
  state: string; last_connected_at?: string; last_heartbeat_at?: string; last_event_at?: string; error_code?: string
}
export interface QQConfig {
  enabled: boolean; app_id: string; api_url: string; model: string; size: string; price: string; allowed_groups: string[]
  public_base_url: string; user_daily_limit: number; user_daily_amount: string; group_concurrency: number
  global_concurrency: number; queue_limit: number; queue_timeout_seconds: number; deadline_seconds: number
  retention_days: number; prompt_max_chars: number; bot_secret_configured: boolean; api_key_configured: boolean; encryption_available: boolean
  image_hosts?: string[]
  receive_mode?: QQReceiveMode
}
export type QQConfigInput = Omit<QQConfig, 'bot_secret_configured' | 'api_key_configured' | 'encryption_available'> & { bot_secret?: string; api_key?: string; clear_bot_secret?: boolean; clear_api_key?: boolean }
export interface QQDiscoveryCandidate { group_openid: string; event_id: string; received_at: string }
export interface QQDiscovery {
  id: string; status: 'waiting' | 'found' | 'confirmed' | 'closed' | 'expired'; app_id: string; expires_at: string
  candidate?: QQDiscoveryCandidate
}
export interface QQDiscoveryStart extends QQDiscovery { command: string }
export interface QQAudit { id: string | number; action: string; actor_id?: number; order_id?: string; object_id?: string; created_at: string; detail?: string; reason?: string }

export interface QQReplyTemplate {
  key: string; label: string; label_en?: string; category: string; default_text: string
  allowed_variables: string[]; required_variables: string[]; variable_limits: Record<string, number>
}
export interface QQReplyTemplates {
  schema_version: number; catalog_version: number; revision: number; overrides: Record<string, string>; catalog: QQReplyTemplate[]
}
export interface QQReplyTemplateInput { expected_revision: number; overrides: Record<string, string> }
export interface QQReplyPreview { valid: boolean; text: string; length: number; worst_length: number; max_length: number; errors: string[] }
export interface QQReplyRevision { revision: number; created_at: string; actor_id: number; restored_from_revision?: number }
export interface QQReplyRevisionDetail extends QQReplyRevision { overrides: Record<string, string> }
async function request<T>(method: string, url: string, data?: unknown, signal?: AbortSignal, binary = false): Promise<T> {
  const owner = ownedAuthRequestConfig()
  let storedSession: string | null
  try { storedSession = localStorage.getItem('auth_session_id') }
  catch { throw new CanceledError('Authentication session is unavailable') }
  // A queued interceptor can yield before dispatch. Check again at the adapter boundary.
  const verify: AxiosRequestTransformer = body => {
    try {
      if (storedSession !== localStorage.getItem('auth_session_id') || JSON.parse(localStorage.getItem('auth_user') || 'null')?.id !== owner.authIdentity.userID) throw new Error()
    } catch { throw new CanceledError('Authentication session changed before dispatch') }
    return body
  }
  const defaults = apiClient.defaults.transformRequest
  const config: AxiosRequestConfig = {
    ...owner, method, url, data, signal, ...(binary ? { responseType: 'blob' } : {}),
    transformRequest: [verify, ...(Array.isArray(defaults) ? defaults : defaults ? [defaults] : [])]
  }
  const response = await apiClient.request<T>(config)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  try {
    if (storedSession !== localStorage.getItem('auth_session_id') || JSON.parse(localStorage.getItem('auth_user') || 'null')?.id !== owner.authIdentity.userID) throw new Error()
  } catch { throw { code: 'AUTH_SESSION_CHANGED', status: 401 } }
  return response.data
}
const user = '/user/qq-image'
const admin = '/admin/qq-image'
const id = encodeURIComponent
export const qqImageAPI = {
  status: (signal?: AbortSignal) => request<QQStatus>('get', `${user}/status`, undefined, signal),
  request: (value: string, signal?: AbortSignal) => request<QQBindingRequest>('get', `${user}/requests/${id(value)}`, undefined, signal),
  authorize: (value: string, signal?: AbortSignal) => request<QQConfirmation>('post', `${user}/requests/${id(value)}/authorize`, { consent: true }, signal),
  sendEmailCode: (signal?: AbortSignal) => request<void>('post', `${user}/email/send-code`, {}, signal),
  verifyEmail: (code: string, signal?: AbortSignal) => request<void>('post', `${user}/email/verify`, { code }, signal),
  revoke: (value: string, signal?: AbortSignal) => request<void>('delete', `${user}/grants/${id(value)}`, undefined, signal),
  orders: (signal?: AbortSignal) => request<QQOrder[]>('get', `${user}/orders`, undefined, signal),
  order: (value: string, signal?: AbortSignal) => request<QQOrder>('get', `${user}/orders/${id(value)}`, undefined, signal),
  cancel: (value: string, signal?: AbortSignal) => request<void>('post', `${user}/orders/${id(value)}/cancel`, {}, signal),
  image: (value: string, signal?: AbortSignal) => request<Blob>('get', `${user}/orders/${id(value)}/image`, undefined, signal, true)
}
export const adminQQImageAPI = {
  replyTemplates: (signal?: AbortSignal) => request<QQReplyTemplates>('get', `${admin}/reply-templates`, undefined, signal),
  saveReplyTemplates: (data: QQReplyTemplateInput, signal?: AbortSignal) => request<QQReplyTemplates>('put', `${admin}/reply-templates`, data, signal),
  previewReplyTemplate: (data: { key: string; text: string }, signal?: AbortSignal) => request<QQReplyPreview>('post', `${admin}/reply-templates/preview`, data, signal),
  replyTemplateRevisions: (signal?: AbortSignal) => request<QQReplyRevision[]>('get', `${admin}/reply-templates/revisions`, undefined, signal),
  replyTemplateRevision: (revision: number, signal?: AbortSignal) => request<QQReplyRevisionDetail>('get', `${admin}/reply-templates/revisions/${id(String(revision))}`, undefined, signal),
  rollbackReplyTemplates: (data: { expected_revision: number; revision: number }, signal?: AbortSignal) => request<QQReplyTemplates>('post', `${admin}/reply-templates/rollback`, data, signal),
  connection: (signal?: AbortSignal) => request<QQConnection>('get', `${admin}/connection`, undefined, signal),
  discovery: (signal?: AbortSignal) => request<QQDiscovery | null>('get', `${admin}/discovery`, undefined, signal),
  startDiscovery: (signal?: AbortSignal) => request<QQDiscoveryStart>('post', `${admin}/discovery/start`, {}, signal),
  closeDiscovery: (value: string, signal?: AbortSignal) => request<QQDiscovery>('post', `${admin}/discovery/${id(value)}/close`, {}, signal),
  confirmDiscovery: (value: string, signal?: AbortSignal) => request<QQDiscovery>('post', `${admin}/discovery/${id(value)}/confirm`, {}, signal),
  config: (signal?: AbortSignal) => request<QQConfig>('get', `${admin}/config`, undefined, signal),
  saveConfig: (data: QQConfigInput, signal?: AbortSignal) => request<QQConfig>('put', `${admin}/config`, data, signal),
  orders: (signal?: AbortSignal) => request<QQOrder[]>('get', `${admin}/orders`, undefined, signal),
  grants: (signal?: AbortSignal) => request<QQGrant[]>('get', `${admin}/grants`, undefined, signal),
  audit: (signal?: AbortSignal) => request<QQAudit[]>('get', `${admin}/audit`, undefined, signal),
  refund: (value: string, reason: string, signal?: AbortSignal) => request<void>('post', `${admin}/orders/${id(value)}/refund`, { reason }, signal),
  reconcile: (value: string, signal?: AbortSignal) => request<void>('post', `${admin}/orders/${id(value)}/reconcile`, {}, signal),
  release: (value: string, reason: string, signal?: AbortSignal) => request<void>('post', `${admin}/orders/${id(value)}/release`, { reason }, signal),
  revoke: (value: string, reason: string, signal?: AbortSignal) => request<void>('delete', `${admin}/grants/${id(value)}`, { reason }, signal),
  image: (value: string, signal?: AbortSignal) => request<Blob>('get', `${admin}/orders/${id(value)}/image`, undefined, signal, true)
}
