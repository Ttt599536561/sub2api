import { describe, expect, it } from 'vitest'
import { validateInviteTemplates, parseInviteCodes, connectionState, inviteError } from '../qqInvite'
import zh from '@/i18n/locales/zh/qqInvite'
import en from '@/i18n/locales/en/qqInvite'
describe('NapCat invitation safety', () => {
  it('never equates process readiness with QQ online', () => {
    expect(connectionState({ online: false, core_ready: true })).toBe('offline')
    expect(connectionState(null)).toBe('unknown')
    expect(connectionState({ online: true, core_ready: true })).toBe('online')
  })
  it.each([
    ['waiting_qrcode', false, 'waiting_scan'],
    ['generating_qrcode', false, 'waiting_scan'],
    ['qrcode_scanned', false, 'waiting_confirmation'],
    ['unknown', true, 'waiting_confirmation'],
    ['initializing', true, 'initializing'],
    ['reconnecting', true, 'reconnecting'],
    ['offline', false, 'offline'],
    ['UNTRUSTED-PHASE', false, 'offline']
  ] as const)('maps login phase %s with QR accepted=%s to %s', (login_phase, qr_login_accepted, expected) => {
    expect(connectionState({ online: false, core_ready: false, login_phase, qr_login_accepted })).toBe(expected)
  })
  it('prioritizes a confirmed online state over stale QR progress', () => {
    expect(connectionState({ online: true, core_ready: true, login_phase: 'qrcode_scanned', qr_login_accepted: true })).toBe('online')
  })
  it.each([
    ['NAPCAT_NOT_CONFIGURED', 'notConfigured'],
    ['NAPCAT_UNAVAILABLE', 'unavailable'],
    ['NAPCAT_UNAUTHORIZED', 'unauthorized'],
    ['NAPCAT_TWO_FACTOR', 'twoFactor'],
    ['NAPCAT_PROTOCOL_ERROR', 'protocol'],
    ['QQ_INVITE_ENCRYPTION_UNAVAILABLE', 'encryption'],
    ['NAPCAT_OFFLINE', 'offline'],
    ['NAPCAT_RATE_LIMITED', 'rateLimited']
  ])('maps %s to an actionable translated error without reflecting private messages', (code, expected) => {
    expect(inviteError({ status: 502, code, message: 'PRIVATE TOKEN' })).toBe(`connectionErrors.${expected}`)
    expect(inviteError({ response: { status: 502, data: { code, message: 'PRIVATE TOKEN' } } })).toBe(`connectionErrors.${expected}`)
    expect(inviteError({ code: 'ERR_BAD_RESPONSE', response: { status: 502, data: { code } } })).toBe(`connectionErrors.${expected}`)
  })
  it.each([
    ['NAPCAT_NOT_CONFIGURED', 503, 'connectionErrors.notConfigured'],
    ['NAPCAT_UNAVAILABLE', 502, 'connectionErrors.unavailable'],
    ['NAPCAT_UNAUTHORIZED', 502, 'connectionErrors.unauthorized'],
    ['NAPCAT_TWO_FACTOR', 502, 'connectionErrors.twoFactor'],
    ['NAPCAT_PROTOCOL_ERROR', 502, 'connectionErrors.protocol'],
    ['QQ_INVITE_ENCRYPTION_UNAVAILABLE', 503, 'connectionErrors.encryption'],
    ['NAPCAT_OFFLINE', 409, 'connectionErrors.offline'],
    ['NAPCAT_RATE_LIMITED', 429, 'connectionErrors.rateLimited'],
    ['QQ_INVITE_CAMPAIGN_GROUP_LOCKED', 409, 'campaignGroupLocked']
  ])('maps the actual numeric-code response reason %s in normalized and Axios errors', (reason, status, expected) => {
    const envelope = { code: status, reason, message: 'PRIVATE TOKEN' }
    // response.ErrorWithDetails uses a numeric code; api/client preserves reason on rejection.
    expect(inviteError({ status, ...envelope })).toBe(expected)
    expect(inviteError({ code: 'ERR_BAD_RESPONSE', response: { status, data: envelope } })).toBe(expected)
  })
  it('prioritizes known reasons over legacy string codes', () => {
    expect(inviteError({ status: 409, code: 'NAPCAT_UNAVAILABLE', reason: 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED' })).toBe('campaignGroupLocked')
    expect(inviteError({ status: 502, code: 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED', reason: 'NAPCAT_UNAVAILABLE' })).toBe('connectionErrors.unavailable')
    expect(inviteError({ code: 'NAPCAT_UNAVAILABLE', response: { status: 409, data: { code: 'NAPCAT_UNAUTHORIZED', reason: 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED' } } })).toBe('campaignGroupLocked')
  })
  it('allows known legacy codes as a fallback for unrecognized reasons without reflecting them', () => {
    expect(inviteError({ status: 502, reason: 'PRIVATE REASON', code: 'NAPCAT_UNAUTHORIZED', message: 'PRIVATE TOKEN' })).toBe('connectionErrors.unauthorized')
    expect(inviteError({ response: { status: 409, data: { reason: 'PRIVATE REASON', code: 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED', message: 'PRIVATE TOKEN' } } })).toBe('campaignGroupLocked')
  })
  it.each(['PRIVATE REASON', '__proto__', 'constructor', 'toString', { message: 'PRIVATE REASON' }, null])('safely ignores an unrecognized reason %j', reason => {
    expect(inviteError({ status: 502, code: 502, reason, message: 'PRIVATE TOKEN' })).toBe('failed')
    expect(inviteError({ response: { status: 502, data: { code: 502, reason, message: 'PRIVATE TOKEN' } } })).toBe('failed')
  })
  it('preserves conflicts and safely falls back on unknown errors', () => {
    expect(inviteError({ status: 409, code: 409, reason: 'QQ_INVITE_CONFLICT', message: 'PRIVATE TOKEN' })).toBe('conflict')
    expect(inviteError({ response: { status: 409, data: { code: 409, reason: 'QQ_INVITE_CONFLICT', message: 'PRIVATE TOKEN' } } })).toBe('conflict')
    expect(inviteError({ status: 409, code: 'QQ_INVITE_CONFLICT' })).toBe('conflict')
    expect(inviteError({ response: { status: 409, data: {} } })).toBe('conflict')
    expect(inviteError({ code: 'PRIVATE', message: 'PRIVATE TOKEN' })).toBe('failed')
    expect(inviteError(null)).toBe('failed')
  })
  it('maps a locked subscription group to a business error instead of a generic conflict or connection error', () => {
    const code = 'QQ_INVITE_CAMPAIGN_GROUP_LOCKED'
    expect(inviteError({ status: 409, code })).toBe('campaignGroupLocked')
    expect(inviteError({ response: { status: 409, data: { code } } })).toBe('campaignGroupLocked')
    expect(inviteError({ code: 'ERR_BAD_RESPONSE', response: { status: 409, data: { code } } })).toBe('campaignGroupLocked')
  })
  it('provides accurate draft eligibility and create-new-campaign guidance in both languages', () => {
    expect(zh.qqInvite.subscriptionGroupHint).toBe('仅从未启用、无月卡库存且无邀请历史的草稿活动可修改订阅分组；其他活动如需更换分组，请创建新活动。')
    expect(zh.qqInvite.campaignGroupLocked).toBe('此活动已启用过，或已有月卡库存或邀请历史，不能修改订阅分组。如需更换分组，请创建新活动。')
    expect(en.qqInvite.subscriptionGroupHint).toBe('Only drafts that have never been enabled and have no pass inventory or invitation history can change subscription groups. Otherwise, create a new campaign for a different group.')
    expect(en.qqInvite.campaignGroupLocked).toBe('This campaign has been enabled before, or has pass inventory or invitation history, so its subscription group cannot be changed. Create a new campaign for a different group.')
  })
  it('rejects redeem codes in any group template and requires private code and URL', () => {
    const base = { welcome_template: 'Welcome {invitee}', congratulation_template: 'Thanks {inviter}', pending_template: 'Pending', delivery_pending_template: 'Please add the bot as a friend', private_template: '{code} {url}' }
    expect(validateInviteTemplates(base)).toBe('')
    expect(validateInviteTemplates({ ...base, congratulation_template: '{inviter} {plan_name}' })).toBe('')
    for (const key of ['welcome_template', 'congratulation_template', 'pending_template', 'delivery_pending_template']) expect(validateInviteTemplates({ ...base, [key]: '{code}' })).toBe('groupCode')
    expect(validateInviteTemplates({ ...base, private_template: '{code}' })).toBe('privateRequired')
    expect(validateInviteTemplates({ ...base, private_template: '{url}' })).toBe('privateRequired')
    expect(validateInviteTemplates({ ...base, private_template: '{code} {url} {unknown}' })).toBe('unknownVariable')
  })
  it('preserves duplicates for server preflight and supports pasted whitespace', () => {
    expect(parseInviteCodes(' a\r\nb  c\n a ')).toEqual(['a', 'b', 'c', 'a'])
  })
})
