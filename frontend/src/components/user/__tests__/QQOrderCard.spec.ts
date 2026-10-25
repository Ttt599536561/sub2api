import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QQOrderCard from '../QQOrderCard.vue'
import zh from '@/i18n/locales/zh/qqImage'
import en from '@/i18n/locales/en/qqImage'
const language = { value: 'en' }
function message(key: string) { return key.split('.').reduce<unknown>((value, part) => value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, language.value === 'zh' ? zh : en) }
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => message(key) || key, te: (key: string) => typeof message(key) === 'string' }) }))
const order = { id: 'order', prompt: '', price: '0.12', model: 'image', size: '1024x1024', status: 'closed_unknown', funds_status: 'release_pending', delivery_status: 'site_only', created_at: '2026-10-23T00:00:00Z', deadline: '2026-10-23T02:00:00Z' }
describe('QQ order protocol status labels', () => {
  it.each([
    ['zh', '仅在网站领取', '核查到期', '查看资金状态', '释放恢复中'],
    ['en', 'Collect on this website only', 'Review deadline reached', 'check funds status', 'Release pending']
  ])('maps site_only and closed_unknown without treating pending funds as returned (%s)', (locale, delivery, task, fundsHint, pending) => {
    language.value = locale
    const wrapper = mount(QQOrderCard, { props: { order, detail: true }, global: { stubs: { RouterLink: true } } })
    expect(wrapper.text()).toContain(delivery); expect(wrapper.text()).toContain(task)
    expect(wrapper.text()).toContain(fundsHint); expect(wrapper.text()).toContain(pending)
    wrapper.unmount()
  })
  it('does not map arbitrary unknown tasks to closed or released', () => {
    language.value = 'en'
    const wrapper = mount(QQOrderCard, { props: { order: { ...order, status: 'future_state', funds_status: 'held' }, detail: true }, global: { stubs: { RouterLink: true } } })
    expect(wrapper.text()).toContain('Awaiting status confirmation'); expect(wrapper.text()).toContain('Reserved')
    expect(wrapper.text()).not.toContain('Review deadline reached'); expect(wrapper.text()).not.toContain('Refunded')
    wrapper.unmount()
  })
})
