/** QQ login flows accept fixed local destinations only, never arbitrary redirect URLs. */
export function qqReturnPath(value: unknown): string {
  return value === '/qq/bind' || value === '/qq/orders' ? value : ''
}
export function safeAuthReturnPath(value: unknown): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\') || [...value].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return '/dashboard'
  return value
}
export function qqAuthTarget(path: '/login' | '/register' | '/email-verify', redirect: unknown) {
  const fixed = qqReturnPath(redirect)
  return fixed ? { path, query: { redirect: fixed } } : { path }
}
const requestKey = 'qq-image.binding-request'
export function validQQRequest(value: unknown): string {
  return typeof value === 'string' && /^[a-zA-Z0-9_-]{1,160}$/.test(value) ? value : ''
}
export function rememberQQRequest(value: string, now = Date.now()) {
  try {
    if (validQQRequest(value)) sessionStorage.setItem(requestKey, JSON.stringify({ id: value, expires: now + 30 * 60 * 1000 }))
    else sessionStorage.removeItem(requestKey)
  } catch { /* Manual request entry remains available without browser storage. */ }
}
export function restoreQQRequest(now = Date.now()): string {
  try {
    const saved = JSON.parse(sessionStorage.getItem(requestKey) || 'null')
    if (saved && Number.isFinite(saved.expires) && saved.expires > now) return validQQRequest(saved.id)
    sessionStorage.removeItem(requestKey)
  } catch { /* A malformed or unavailable session never grants authorization. */ }
  return ''
}
export function qqErrorKey(error: unknown, context: 'general' | 'binding' = 'general'): string {
  const value = error as { status?: number; code?: string; reason?: string }
  const code = String(value?.reason || value?.code || '').toUpperCase()
  switch (code) {
    case 'QQ_IMAGE_NOT_FOUND': return context === 'binding' ? 'requestNotFound' : 'notFound'
    case 'QQ_IMAGE_INVALID': return 'invalid'
    case 'QQ_IMAGE_DISABLED': return context === 'binding' ? 'bindingDisabled' : 'disabled'
    case 'QQ_IMAGE_ENCRYPTION_UNAVAILABLE': return 'unavailable'
    case 'EMAIL_NOT_CONFIGURED': return 'emailNotConfigured'
    case 'EMAIL_RESERVED': return 'emailReserved'
    case 'INVALID_VERIFY_CODE': return 'invalidVerifyCode'
    case 'VERIFY_CODE_MAX_ATTEMPTS': return 'verifyCodeMaxAttempts'
    case 'VERIFY_CODE_TOO_FREQUENT': return 'rate'
    case 'EMAIL_VERIFY_REQUIRED': return 'email'
  }
  if (value?.status === 409 || /CONFLICT|CLAIMED|BOUND/.test(code)) return 'conflict'
  if (value?.status === 410 || /EXPIRED|INVALID_REQUEST/.test(code)) return 'expired'
  if (/RECENT|REAUTH/.test(code)) return 'recent'
  if (value?.status === 429) return 'rate'
  if (value?.status === 403) return 'forbidden'
  return 'failed'
}
