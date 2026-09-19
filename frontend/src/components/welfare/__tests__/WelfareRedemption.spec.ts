import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import WelfareRedemption from '../WelfareRedemption.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const makeQuote = vi.fn()
const redeem = vi.fn()
function create() { return mount(WelfareRedemption, { props: { show: true, balance: '2.80', busy: false, quoteRequest: makeQuote, redeemRequest: redeem }, global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' } } } }) }
beforeEach(() => {
  vi.clearAllMocks()
  makeQuote.mockResolvedValue({ amount: '2.80', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '21.15', welfare_balance_version: 7 })
  redeem.mockResolvedValue({ status: 'completed', operation_id: 'one', overview: {} })
})
describe('welfare redemption', () => {
  it('freezes the quoted amount while retrying an uncertain redemption', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    redeem.mockResolvedValueOnce(null)
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(wrapper.get('input').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="redeem-all"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="confirm-redeem"]').text()).toBe('welfare.retryTransfer')
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(redeem.mock.calls[1][0]).toEqual(redeem.mock.calls[0][0])
  })
  it('quotes all funds then submits the exact server amount and version', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    expect(makeQuote).toHaveBeenCalledWith({ mode: 'all' })
    expect(wrapper.text()).toContain('$21.15')
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(redeem).toHaveBeenCalledWith({ amount: '2.80', welfare_balance_version: 7 })
    expect(wrapper.emitted('completed')).toHaveLength(1)
  })
  it('accepts a cent and rejects excess precision before requesting a quote', async () => {
    const wrapper = create()
    const amount = wrapper.get('input')
    await amount.setValue('0.001')
    expect(wrapper.get('[data-testid="quote-partial"]').attributes('disabled')).toBeDefined()
    await amount.setValue('0.01')
    await wrapper.get('[data-testid="quote-partial"]').trigger('click'); await flushPromises()
    expect(makeQuote).toHaveBeenCalledWith({ mode: 'partial', amount: '0.01' })
  })
  it('invalidates a displayed quote when amount changes', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    await wrapper.get('input').setValue('1.00')
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(false)
  })
  it('never confirms an old quote arriving after an amount edit', async () => {
    let resolve!: (value: unknown) => void
    makeQuote.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const wrapper = create()
    await wrapper.get('input').setValue('1.00')
    await wrapper.get('[data-testid="quote-partial"]').trigger('click')
    await wrapper.get('input').setValue('2.00')
    resolve({ amount: '1.00', welfare_balance_version: 7 }); await flushPromises()
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(false)
  })
})
