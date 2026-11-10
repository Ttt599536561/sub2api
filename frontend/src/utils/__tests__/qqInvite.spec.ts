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
  it('preserves conflicts and safely falls back on unknown errors', () => {
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
