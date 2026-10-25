import { describe, expect, it } from 'vitest'
import type { QQConfigInput } from '@/api/qqImage'
import { qqConfigNumberLimits, qqConfigSaveProblem, validateQQConfigInput } from '../qqImageConfig'

const saved = { bot_secret_configured: true, api_key_configured: true, encryption_available: true }
function valid(overrides: Partial<QQConfigInput> = {}): QQConfigInput {
  return { enabled: true, receive_mode: 'websocket', app_id: 'app1', api_url: 'https://images.example.test/v1', model: 'image-1', size: '1024x1024', price: '0.04000000', allowed_groups: ['group1'], public_base_url: 'https://site.example.test', user_daily_limit: 20, user_daily_amount: '10.00000000', group_concurrency: 2, global_concurrency: 5, queue_limit: 50, queue_timeout_seconds: 300, deadline_seconds: 7200, retention_days: 7, prompt_max_chars: 2000, ...overrides }
}

describe('QQ configuration preflight', () => {
  it.each(Object.entries(qqConfigNumberLimits))('checks both boundaries and integer format for %s', (field, max) => {
    for (const value of [0, -1, 1.5, max + 1, NaN, Infinity]) {
      expect(validateQQConfigInput(valid({ [field]: value }), saved)).toEqual({ field, issue: 'out_of_range' })
    }
    for (const value of [1, max]) expect(validateQQConfigInput(valid({ [field]: value }), saved)).toBeNull()
  })
  it.each(['price', 'user_daily_amount'] as const)('validates nonempty %s even when disabled', field => {
    for (const value of ['0', '-1', '1e2', '0.000000001', '1,000', '$0.04', '1000000000000']) {
      expect(validateQQConfigInput(valid({ enabled: false, [field]: value }), saved)).toEqual({ field, issue: 'invalid_amount' })
    }
    for (const value of ['', '0.00000001', '999999999999.99999999']) {
      expect(validateQQConfigInput(valid({ enabled: false, [field]: value }), saved)).toBeNull()
    }
  })
  it.each(['api_url', 'public_base_url'] as const)('validates nonempty %s without changing the draft', field => {
    for (const value of ['http://example.test', 'https://localhost', 'https://a.local', 'https://127.0.0.1', 'https://192.168.1.2', 'https://169.254.2.3', 'https://[::1]', 'https://[::ffff:127.0.0.1]', 'https://[fc00::1]', 'https://[fe80::1]', 'https://[ff00::1]', 'https://user:password@example.test', 'https://example.test/?key=secret', 'https://example.test/#fragment']) {
      const input = valid({ enabled: false, [field]: value })
      const original = structuredClone(input)
      expect(validateQQConfigInput(input, saved)).toEqual({ field, issue: 'invalid_url' })
      expect(input).toEqual(original)
    }
    expect(validateQQConfigInput(valid({ enabled: false, [field]: '' }), saved)).toBeNull()
    expect(validateQQConfigInput(valid({ [field]: 'https://example.test:443' }), saved)).toBeNull()
  })
  it('distinguishes a full generation endpoint from the website path', () => {
    expect(validateQQConfigInput(valid({ enabled: false, api_url: 'https://api.example.test/v1/images/generations///' }), saved)).toEqual({ field: 'api_url', issue: 'generation_endpoint' })
    expect(validateQQConfigInput(valid({ public_base_url: 'https://site.example.test/admin' }), saved)).toEqual({ field: 'public_base_url', issue: 'invalid_url' })
  })
  it('does not let browser URL normalization hide an invalid origin or empty credentials', () => {
    for (const value of ['https://site.example.test/.', 'https://site.example.test/a/..', 'https://@site.example.test']) {
      expect(validateQQConfigInput(valid({ public_base_url: value }), saved)).toEqual({ field: 'public_base_url', issue: 'invalid_url' })
    }
  })
  it('preserves existing nondefault-port compatibility while disabled', () => {
    expect(validateQQConfigInput(valid({ enabled: false, api_url: 'https://api.example.test:8443/v1' }), saved)).toBeNull()
    expect(validateQQConfigInput(valid({ api_url: 'https://api.example.test:8443/v1' }), saved)).toEqual({ field: 'api_url', issue: 'invalid_url' })
  })
  it('uses byte bounds for app IDs, bot secrets and official group identifiers', () => {
    expect(validateQQConfigInput(valid({ app_id: '汉'.repeat(86) }), saved)).toEqual({ field: 'app_id', issue: 'invalid_format' })
    expect(validateQQConfigInput(valid({ app_id: 'app id' }), saved)).toEqual({ field: 'app_id', issue: 'invalid_format' })
    expect(validateQQConfigInput(valid({ bot_secret: '🙂'.repeat(1025) }), saved)).toEqual({ field: 'bot_secret', issue: 'invalid_format' })
    expect(validateQQConfigInput(valid({ allowed_groups: ['汉'.repeat(86)] }), saved)).toEqual({ field: 'allowed_groups', issue: 'invalid_format' })
    expect(validateQQConfigInput(valid({ app_id: 'a'.repeat(256), bot_secret: '🙂'.repeat(1024), allowed_groups: ['a'.repeat(256)] }), saved)).toBeNull()
  })
  it('requires only credentials absent from both saved settings and the draft', () => {
    expect(validateQQConfigInput(valid(), { ...saved, bot_secret_configured: false })).toEqual({ field: 'bot_secret', issue: 'missing_credential' })
    expect(validateQQConfigInput(valid(), { ...saved, api_key_configured: false })).toEqual({ field: 'api_key', issue: 'missing_credential' })
    expect(validateQQConfigInput(valid({ api_key: 'fixture-new-key' }), { ...saved, api_key_configured: false })).toBeNull()
    expect(validateQQConfigInput(valid({ enabled: false, api_url: '', model: '', size: '', price: '', public_base_url: '', user_daily_amount: '', allowed_groups: [] }), { ...saved, api_key_configured: false })).toBeNull()
  })
  it('preserves explicit credential removal and rejects conflicting replacements', () => {
    expect(validateQQConfigInput(valid({ clear_api_key: true }), saved)).toBeNull()
    expect(validateQQConfigInput(valid({ clear_api_key: true, api_key: 'new' }), saved)).toEqual({ field: 'api_key', issue: 'conflicting_clear' })
    expect(validateQQConfigInput(valid({ clear_bot_secret: true, bot_secret: 'new' }), saved)).toEqual({ field: 'bot_secret', issue: 'conflicting_clear' })
    expect(validateQQConfigInput(valid({ enabled: false, api_key: 'new' }), { ...saved, encryption_available: false })).toEqual({ field: 'request', issue: 'encryption_unavailable' })
  })
  it('checks every required field and image-host count', () => {
    for (const field of ['app_id', 'api_url', 'model', 'size', 'price', 'public_base_url', 'user_daily_amount'] as const) {
      expect(validateQQConfigInput(valid({ [field]: '' }), saved)).toEqual({ field, issue: 'required' })
    }
    expect(validateQQConfigInput(valid({ allowed_groups: [] }), saved)).toEqual({ field: 'allowed_groups', issue: 'required' })
    expect(validateQQConfigInput(valid({ image_hosts: Array.from({ length: 21 }, (_, i) => `cdn${i}.example.test`) }), saved)).toEqual({ field: 'image_hosts', issue: 'too_many' })
    for (const host of ['*.example.test', 'https://example.test', 'cdn.local', '8.8.8.8', 'cdn.example.test/path']) {
      expect(validateQQConfigInput(valid({ image_hosts: [host] }), saved)).toEqual({ field: 'image_hosts', issue: 'invalid_format' })
    }
  })
})

describe('QQ configuration error boundaries', () => {
  it('only consumes a matching controlled status, reason and field/issue pair', () => {
    const metadata = { field: 'retention_days', issue: 'out_of_range', value: 'PRIVATE' }
    expect(qqConfigSaveProblem({ status: 400, reason: 'QQ_IMAGE_CONFIG_INVALID', metadata })).toEqual({ field: 'retention_days', issue: 'out_of_range' })
    expect(qqConfigSaveProblem({ status: 500, reason: 'QQ_IMAGE_CONFIG_INVALID', metadata })).toEqual({ field: 'request', issue: 'uncertain' })
    expect(qqConfigSaveProblem({ status: 400, reason: 'PRIVATE', metadata })).toEqual({ field: 'request', issue: 'invalid_request' })
  })
  it.each([null, [], { field: '__proto__', issue: 'invalid_format' }, { field: 'constructor', issue: 'invalid_format' }, { field: 'api_key', issue: 'invalid_url' }, { field: 'api_url', issue: ['invalid_url'] }, { field: 'PRIVATE', issue: 'PRIVATE' }])('rejects malformed or unexpected metadata %#', metadata => {
    expect(qqConfigSaveProblem({ status: 400, reason: 'QQ_IMAGE_CONFIG_INVALID', metadata, message: 'PRIVATE' })).toEqual({ field: 'request', issue: 'invalid_request' })
  })
})
