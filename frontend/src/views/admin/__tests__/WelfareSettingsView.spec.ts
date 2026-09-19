import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WelfareSettingsView from '../WelfareSettingsView.vue'

const { getSettings, updateSettings, showError, showSuccess, fetchPublicSettings } = vi.hoisted(() => ({
  getSettings: vi.fn(), updateSettings: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), fetchPublicSettings: vi.fn()
}))
vi.mock('@/api/admin/welfare', () => ({ adminWelfareAPI: { getSettings, updateSettings } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess, fetchPublicSettings }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('WelfareSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getSettings.mockResolvedValue({ enabled: false, launch_at: null, rules_version: 2 })
    updateSettings.mockResolvedValue({ enabled: true, launch_at: '2026-09-19T00:00:00Z', rules_version: 2 })
    fetchPublicSettings.mockResolvedValue({ welfare_enabled: true })
  })
  it('saves the rewards switch and refreshes public availability', async () => {
    const wrapper = mount(WelfareSettingsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('input[type=checkbox]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith({ enabled: true })
    expect(fetchPublicSettings).toHaveBeenCalledWith(true)
    expect(showSuccess).toHaveBeenCalledWith('welfare.admin.saved')
    wrapper.unmount()
  })
  it('does not turn a committed save into a failure when public refresh fails', async () => {
    fetchPublicSettings.mockRejectedValue(new Error('network'))
    const wrapper = mount(WelfareSettingsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(showSuccess).toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
