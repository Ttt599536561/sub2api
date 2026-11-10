import type { InviteConnection, InviteTemplates } from '@/api/qqInvite'
export function connectionState(value: Pick<InviteConnection, 'online' | 'core_ready' | 'login_phase' | 'qr_login_accepted'> | null) {
  if (!value) return 'unknown'
  if (value.online === true) return 'online'
  // A more specific active phase takes precedence over a stale accepted-QR flag.
  if (value.login_phase === 'initializing') return 'initializing'
  if (value.login_phase === 'reconnecting') return 'reconnecting'
  if (value.login_phase === 'qrcode_scanned' || value.qr_login_accepted === true) return 'waiting_confirmation'
  if (value.login_phase === 'waiting_qrcode' || value.login_phase === 'generating_qrcode') return 'waiting_scan'
  return 'offline'
}
export function parseInviteCodes(value: string) { return value.split(/\s+/).filter(Boolean) }
export function validateInviteTemplates(value: InviteTemplates): string {
  if ([value.welcome_template, value.congratulation_template, value.pending_template, value.delivery_pending_template].some(text => /\{\s*code\s*\}/i.test(text))) return 'groupCode'
  if (!value.private_template.includes('{code}') || !value.private_template.includes('{url}')) return 'privateRequired'
  for (const text of Object.values(value)) {
    if (!text.trim() || text.length > 2000) return 'templateLength'
    if (text.replace(/\{(?:invitee|inviter|level|code|url|plan_name)\}/g, '').match(/[{}]/)) return 'unknownVariable'
  }
  return ''
}
export function inviteError(error: unknown) {
  const detail = error as { code?: unknown; status?: number; response?: { status?: number; data?: { code?: unknown } } } | null
  const code = detail?.response?.data?.code ?? detail?.code
  const errors: Record<string, string> = {
    NAPCAT_NOT_CONFIGURED: 'notConfigured', NAPCAT_UNAVAILABLE: 'unavailable',
    NAPCAT_UNAUTHORIZED: 'unauthorized', NAPCAT_TWO_FACTOR: 'twoFactor',
    NAPCAT_PROTOCOL_ERROR: 'protocol', QQ_INVITE_ENCRYPTION_UNAVAILABLE: 'encryption',
    NAPCAT_OFFLINE: 'offline', NAPCAT_RATE_LIMITED: 'rateLimited'
  }
  if (typeof code === 'string' && Object.prototype.hasOwnProperty.call(errors, code)) return `connectionErrors.${errors[code]}`
  const status = detail?.response?.status ?? detail?.status
  return status === 409 ? 'conflict' : 'failed'
}
export function localDateTime(iso: string) {
  const date = new Date(iso)
  return Number.isFinite(date.getTime()) ? new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16) : ''
}
