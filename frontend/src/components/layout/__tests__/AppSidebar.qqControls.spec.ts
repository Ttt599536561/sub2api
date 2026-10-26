import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, reactive } from 'vue'
import { mount } from '@vue/test-utils'
import { useAppStore } from '@/stores/app'
import type { PublicSettings } from '@/types'
import AppSidebar from '../AppSidebar.vue'

const auth = reactive({ isAdmin: false, isSimpleMode: false })

vi.mock('@/api/admin/system', () => ({ checkUpdates: vi.fn() }))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))
vi.mock('@/stores', async () => ({
  useAppStore: (await import('@/stores/app')).useAppStore,
  useAuthStore: () => auth,
  useAdminSettingsStore: () => ({ fetch: vi.fn(), opsMonitoringEnabled: false, paymentEnabled: false, customMenuItems: [] }),
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('@/composables/useBatchImageAccess', async () => {
  const { ref } = await import('vue')
  return { useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() }) }
})
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({ useRoute: () => ({ path: '/dashboard' }), useRouter: () => ({ push: vi.fn() }) }))

const RouterLinkStub = defineComponent({
  props: { to: { type: String, required: true } },
  setup: (props, { slots }) => () => h('a', { href: props.to }, slots.default?.()),
})

describe('QQ history navigation while new business is paused', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    delete window.__APP_CONFIG__
    auth.isAdmin = false
    auth.isSimpleMode = false
  })

  it.each([false, true])('keeps QQ authorization and history links reachable (admin=%s)', async (isAdmin) => {
    auth.isAdmin = isAdmin
    const store = useAppStore()
    store.cachedPublicSettings = { qq_binding_enabled: true, qq_image_enabled: true } as PublicSettings
    const wrapper = mount(AppSidebar, {
      global: { stubs: { RouterLink: RouterLinkStub, VersionBadge: true }, mocks: { $t: (key: string) => key } },
    })

    for (const settings of [
      { qq_binding_enabled: false, qq_image_enabled: true },
      { qq_binding_enabled: true, qq_image_enabled: false },
      { qq_binding_enabled: false, qq_image_enabled: false },
      {},
    ]) {
      store.cachedPublicSettings = settings as PublicSettings
      await nextTick()
      expect(wrapper.get('a[href="/qq/bind"]').isVisible()).toBe(true)
      expect(wrapper.get('a[href="/qq/orders"]').isVisible()).toBe(true)
    }
    wrapper.unmount()
  })
})
