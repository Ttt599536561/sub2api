import type { QQConfig, QQConfigInput } from '@/api/qqImage'

export const qqConfigNumberLimits = {
  user_daily_limit: 10000, group_concurrency: 100, global_concurrency: 100, queue_limit: 10000,
  queue_timeout_seconds: 3600, deadline_seconds: 7200, retention_days: 30, prompt_max_chars: 2000
} as const

const fieldIssues = {
  request: ['invalid_request'], receive_mode: ['invalid_format'], app_id: ['invalid_format', 'required'],
  bot_secret: ['invalid_format', 'conflicting_clear', 'missing_credential'], api_key: ['conflicting_clear', 'missing_credential'],
  api_url: ['generation_endpoint', 'invalid_url', 'required'], public_base_url: ['invalid_url', 'required'],
  model: ['required'], size: ['required'], price: ['invalid_amount', 'required'], user_daily_amount: ['invalid_amount', 'required'],
  allowed_groups: ['invalid_format', 'required'], image_hosts: ['too_many', 'invalid_format'],
  user_daily_limit: ['out_of_range'], group_concurrency: ['out_of_range'], global_concurrency: ['out_of_range'],
  queue_limit: ['out_of_range'], queue_timeout_seconds: ['out_of_range'], deadline_seconds: ['out_of_range'],
  retention_days: ['out_of_range'], prompt_max_chars: ['out_of_range']
} as const
export type QQConfigField = keyof typeof fieldIssues
export type QQConfigIssue = typeof fieldIssues[QQConfigField][number] | 'encryption_unavailable' | 'forbidden' | 'conflict' | 'uncertain' | 'rate' | 'unauthorized'
export interface QQConfigProblem { field: QQConfigField; issue: QQConfigIssue }

/** Only a known field/issue pair can become UI text; raw response values are never returned. */
export function qqConfigSaveProblem(error: unknown): QQConfigProblem {
  const failure = error as { status?: unknown; reason?: unknown; metadata?: unknown } | null
  if (failure?.status === 400 && failure.reason === 'QQ_IMAGE_CONFIG_INVALID') {
    const meta = failure.metadata as { field?: unknown; issue?: unknown } | null
    if (typeof meta?.field === 'string' && Object.prototype.hasOwnProperty.call(fieldIssues, meta.field) && typeof meta.issue === 'string') {
      const field = meta.field as QQConfigField
      if ((fieldIssues[field] as readonly string[]).includes(meta.issue)) return { field, issue: meta.issue as QQConfigIssue }
    }
  }
  const field = 'request'
  if (failure?.status === 400) return { field, issue: 'invalid_request' }
  if (failure?.status === 503 && failure.reason === 'QQ_IMAGE_ENCRYPTION_UNAVAILABLE') return { field, issue: 'encryption_unavailable' }
  if (failure?.status === 403) return { field, issue: 'forbidden' }
  if (failure?.status === 409) return { field, issue: 'conflict' }
  if (failure?.status === 429) return { field, issue: 'rate' }
  if (failure?.status === 401) return { field, issue: 'unauthorized' }
  return { field, issue: 'uncertain' }
}

const byteLength = (text: string) => new TextEncoder().encode(text).length
const positiveMoney = (value: string) => /^\d{1,12}(\.\d{1,8})?$/.test(value) && /[1-9]/.test(value)

function privateIPv4(host: string): boolean {
  const octets = host.split('.').map(Number)
  return octets.length === 4 && (octets[0] === 10 || octets[0] === 127 || (octets[0] === 0 && octets.every(part => part === 0)) ||
    (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) || (octets[0] === 192 && octets[1] === 168) ||
    (octets[0] === 169 && octets[1] === 254) || (octets[0] >= 224 && octets[0] <= 239))
}
function privateHost(host: string): boolean {
  if (host === 'localhost' || host.endsWith('.localhost') || host.endsWith('.local')) return true
  if (/^[\d.]+$/.test(host)) return privateIPv4(host)
  if (!host.startsWith('[')) return false
  const ip = host.slice(1, -1)
  if (ip === '::' || ip === '::1' || /^f[cd]/.test(ip) || /^fe[89ab]/.test(ip) || /^ff/.test(ip)) return true
  const mapped = /^::ffff:([\da-f]+):([\da-f]+)$/.exec(ip)
  if (mapped) {
    const hi = parseInt(mapped[1], 16), lo = parseInt(mapped[2], 16)
    return privateIPv4(`${hi >> 8}.${hi & 255}.${lo >> 8}.${lo & 255}`)
  }
  return false
}
function validHTTPS(raw: string, origin: boolean, require443: boolean): boolean {
  try {
    const parts = /^https:\/\/([^/?#]+)([^?#]*)/.exec(raw)
    if (!parts || parts[1].includes('@') || /[\s\\]/.test(raw) || (origin && parts[2] !== '' && parts[2] !== '/')) return false
    const url = new URL(raw)
    return url.protocol === 'https:' && !!url.hostname && !url.username && !url.password && !url.hash && !url.search &&
      (!require443 || !url.port) && !privateHost(url.hostname) && (!origin || url.pathname === '/')
  } catch { return false }
}
function validImageHost(host: string): boolean {
  return host.length <= 253 && !/^[\d.]+$/.test(host) &&
    /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(host) &&
    validHTTPS(`https://${host}`, true, true)
}

/** Mirrors existing save constraints; does not probe providers, persist, or change the draft. */
export function validateQQConfigInput(input: QQConfigInput, saved: Pick<QQConfig, 'bot_secret_configured' | 'api_key_configured' | 'encryption_available'>): QQConfigProblem | null {
  const invalid = (field: QQConfigField, issue: QQConfigIssue): QQConfigProblem => ({ field, issue })
  const enabled = input.enabled && !input.clear_bot_secret && !input.clear_api_key
  if (input.receive_mode && !['webhook', 'websocket'].includes(input.receive_mode)) return invalid('receive_mode', 'invalid_format')
  if (byteLength(input.app_id) > 256 || /[ \t\r\n]/.test(input.app_id)) return invalid('app_id', 'invalid_format')
  if (byteLength(input.bot_secret || '') > 4096) return invalid('bot_secret', 'invalid_format')
  if ((input.image_hosts || []).length > 20) return invalid('image_hosts', 'too_many')
  if ((input.image_hosts || []).some(host => !validImageHost(host))) return invalid('image_hosts', 'invalid_format')
  if (input.clear_bot_secret && input.bot_secret) return invalid('bot_secret', 'conflicting_clear')
  if (input.clear_api_key && input.api_key) return invalid('api_key', 'conflicting_clear')
  if (input.api_url.replace(/\/+$/, '').endsWith('/images/generations')) return invalid('api_url', 'generation_endpoint')
  if ((enabled || input.bot_secret || input.api_key) && !saved.encryption_available) return invalid('request', 'encryption_unavailable')
  for (const field of ['price', 'user_daily_amount'] as const) {
    if (input[field] && !positiveMoney(input[field])) return invalid(field, 'invalid_amount')
  }
  for (const field of ['api_url', 'public_base_url'] as const) {
    if (input[field] && !validHTTPS(input[field], field === 'public_base_url', !!enabled)) return invalid(field, 'invalid_url')
  }
  if (enabled) {
    for (const field of ['app_id', 'api_url', 'model', 'size', 'price', 'public_base_url', 'user_daily_amount'] as const) {
      if (!input[field]) return invalid(field, 'required')
    }
    if (!input.allowed_groups.length) return invalid('allowed_groups', 'required')
  }
  for (const field of Object.keys(qqConfigNumberLimits) as (keyof typeof qqConfigNumberLimits)[]) {
    if (!Number.isSafeInteger(input[field]) || input[field] < 1 || input[field] > qqConfigNumberLimits[field]) return invalid(field, 'out_of_range')
  }
  if (input.allowed_groups.some(group => !group.trim() || byteLength(group) > 256)) return invalid('allowed_groups', 'invalid_format')
  if (enabled && !input.bot_secret && !saved.bot_secret_configured) return invalid('bot_secret', 'missing_credential')
  if (enabled && !input.api_key && !saved.api_key_configured) return invalid('api_key', 'missing_credential')
  return null
}
