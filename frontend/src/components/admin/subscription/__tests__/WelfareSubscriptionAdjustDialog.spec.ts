import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { UserSubscription } from '@/types'
import WelfareSubscriptionAdjustDialog from '../WelfareSubscriptionAdjustDialog.vue'

const { getById, adjustWelfare } = vi.hoisted(() => ({ getById: vi.fn(), adjustWelfare: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { subscriptions: { getById, adjustWelfare } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function subscription(overrides: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 8, user_id: 3, group_id: 2, entitlement_kind: 'welfare', status: 'active',
    welfare_grant_count: 2, welfare_adjustment_version: 4, welfare_cycle_closed: false,
    welfare_monthly_limit_usd: 20, monthly_usage_usd: 7,
    expires_at: '2099-01-01T00:00:00Z', user: { email: 'member@example.com' },
    group: { name: 'Welfare group', monthly_limit_usd: 20 }, ...overrides
  } as UserSubscription
}

function mountDialog() {
  const wrapper = mount(WelfareSubscriptionAdjustDialog, {
    props: { subscriptionId: 8 },
    global: { stubs: { BaseDialog: {
      name: 'BaseDialog', props: ['closeOnEscape', 'showCloseButton'], emits: ['close'],
      template: '<section><slot /><footer><slot name="footer" /></footer></section>'
    } } }
  })
  wrappers.push(wrapper)
  return wrapper
}
const wrappers: ReturnType<typeof mountDialog>[] = []

beforeEach(() => {
  getById.mockReset().mockResolvedValue(subscription())
  adjustWelfare.mockReset().mockResolvedValue(subscription({ welfare_adjustment_version: 5 }))
})
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('WelfareSubscriptionAdjustDialog', () => {
  it('loads fresh details, defaults both changes off, and submits only the selected total', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    expect(getById).toHaveBeenCalledWith(8)
    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.text()).toContain('Welfare group')
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !(input.element as HTMLInputElement).checked)).toBe(true)
    await wrapper.get('#welfare-change-total').setValue(true)
    expect((wrapper.get('#welfare-monthly-limit').element as HTMLInputElement).value).toBe('20.00')
    await wrapper.get('#welfare-monthly-limit').setValue('10.50')
    await wrapper.get('#welfare-adjust-reason').setValue('Correct reward quota')
    expect(wrapper.get('[data-test="welfare-new-remaining"]').text()).toContain('$3.50')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare).toHaveBeenCalledWith(8, {
      monthly_limit_usd: 10.5, expected_version: 4, expected_grant_count: 2, reason: 'Correct reward quota'
    }, expect.any(String))
    expect(wrapper.emitted('completed')?.[0]?.[0]).toMatchObject({ welfare_adjustment_version: 5 })
  })

  it('supports zero total and immediate expiry while preserving already used quota', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    await wrapper.get('#welfare-monthly-limit').setValue('0')
    await wrapper.get('#welfare-change-days').setValue(true)
    await wrapper.get('#welfare-remaining-days').setValue('0')
    await wrapper.get('#welfare-adjust-reason').setValue('Stop this benefit')
    expect(wrapper.get('[data-test="welfare-new-remaining"]').text()).toContain('$0.00')
    expect(wrapper.text()).toContain('admin.subscriptions.welfareAdjust.quotaDisabled')
    expect(wrapper.text()).toContain('admin.subscriptions.welfareAdjust.expiresImmediately')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare.mock.calls[0]?.[1]).toEqual({
      monthly_limit_usd: 0, remaining_days: 0, expected_version: 4, expected_grant_count: 2, reason: 'Stop this benefit'
    })
  })

  it.each(['', '-1', '1.001', '0.000000001', '10000000000', 'Infinity', '1e2'])('rejects invalid total %s', async value => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    await wrapper.get('#welfare-monthly-limit').setValue(value)
    await wrapper.get('#welfare-adjust-reason').setValue('Valid reason')
    await wrapper.get('form').trigger('submit')
    expect(adjustWelfare).not.toHaveBeenCalled()
  })

  it.each(['', '-1', '1.5', '36501'])('rejects invalid remaining days %s', async value => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-days').setValue(true)
    await wrapper.get('#welfare-remaining-days').setValue(value)
    await wrapper.get('#welfare-adjust-reason').setValue('Valid reason')
    await wrapper.get('form').trigger('submit')
    expect(adjustWelfare).not.toHaveBeenCalled()
  })

  it('requires a selected change and a 3 to 500 character reason', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-adjust-reason').setValue('Valid reason')
    await wrapper.get('form').trigger('submit')
    expect(adjustWelfare).not.toHaveBeenCalled()
    await wrapper.get('#welfare-change-total').setValue(true)
    for (const reason of ['', 'ab', 'x'.repeat(501)]) {
      await wrapper.get('#welfare-adjust-reason').setValue(reason)
      await wrapper.get('form').trigger('submit')
    }
    expect(adjustWelfare).not.toHaveBeenCalled()
  })

  it.each([
    { welfare_adjustment_version: undefined }, { welfare_grant_count: undefined },
    { welfare_cycle_closed: undefined }, { welfare_cycle_closed: true },
    { entitlement_kind: 'standard' }, { status: 'revoked' },
    { welfare_monthly_limit_usd: undefined, group: undefined }
  ] as Partial<UserSubscription>[])('refuses unsafe or incomplete fresh details: %s', async overrides => {
    getById.mockResolvedValue(subscription(overrides))
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(adjustWelfare).not.toHaveBeenCalled()
  })

  it('accepts a scoped DTO with flat quota and no group', async () => {
    getById.mockResolvedValue(subscription({ group: undefined }))
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-days').setValue(true)
    await wrapper.get('#welfare-remaining-days').setValue('36500')
    await wrapper.get('#welfare-adjust-reason').setValue('Extend benefit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare.mock.calls[0]?.[1]).toEqual({
      remaining_days: 36500, expected_version: 4, expected_grant_count: 2, reason: 'Extend benefit'
    })
  })

  it('prefers a flat zero total to the group quota and rounds remaining days up', async () => {
    getById.mockResolvedValue(subscription({ welfare_monthly_limit_usd: 0, expires_at: new Date(Date.now() + 36 * 3600000).toISOString() }))
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    expect((wrapper.get('#welfare-monthly-limit').element as HTMLInputElement).value).toBe('0.00')
    await wrapper.get('#welfare-change-days').setValue(true)
    expect((wrapper.get('#welfare-remaining-days').element as HTMLInputElement).value).toBe('2')
  })

  it('prevents duplicate submits and closing during the request', async () => {
    let resolve!: (value: UserSubscription) => void
    adjustWelfare.mockReturnValue(new Promise<UserSubscription>(done => { resolve = done }))
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    await wrapper.get('#welfare-adjust-reason').setValue('Correct benefit')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    const dialog = wrapper.findComponent({ name: 'BaseDialog' })
    dialog.vm.$emit('close')
    expect(adjustWelfare).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(dialog.props('closeOnEscape')).toBe(false)
    resolve(subscription())
    await flushPromises()
  })

  it('retains input after conflict and requires explicit reload without auto retry', async () => {
    adjustWelfare.mockRejectedValueOnce({ status: 409 })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    await wrapper.get('#welfare-monthly-limit').setValue('12')
    await wrapper.get('#welfare-adjust-reason').setValue('Correct benefit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const oldKey = adjustWelfare.mock.calls[0]?.[2]
    expect(wrapper.text()).toContain('admin.subscriptions.welfareAdjust.conflict')
    expect((wrapper.get('#welfare-monthly-limit').element as HTMLInputElement).value).toBe('12')
    await wrapper.get('form').trigger('submit')
    expect(adjustWelfare).toHaveBeenCalledTimes(1)
    getById.mockResolvedValue(subscription({ welfare_adjustment_version: 5, welfare_grant_count: 3, welfare_monthly_limit_usd: 30 }))
    await wrapper.get('[data-test="welfare-reload"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('#welfare-monthly-limit').element as HTMLInputElement).value).toBe('12')
    expect((wrapper.get('#welfare-adjust-reason').element as HTMLTextAreaElement).value).toBe('Correct benefit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare.mock.calls[1]?.[1]).toMatchObject({ monthly_limit_usd: 12, expected_version: 5, expected_grant_count: 3 })
    expect(adjustWelfare.mock.calls[1]?.[2]).not.toBe(oldKey)
  })

  it('reuses the key after an uncertain outcome and creates a new one for changed input', async () => {
    adjustWelfare.mockRejectedValue({ status: 503 })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('#welfare-change-total').setValue(true)
    await wrapper.get('#welfare-adjust-reason').setValue('Correct benefit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare.mock.calls[1]).toEqual(adjustWelfare.mock.calls[0])
    await wrapper.get('#welfare-monthly-limit').setValue('5')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adjustWelfare.mock.calls[2]?.[2]).not.toBe(adjustWelfare.mock.calls[0]?.[2])
  })
})
