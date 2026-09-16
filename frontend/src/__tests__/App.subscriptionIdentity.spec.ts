import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '@/App.vue'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { UserSubscription } from '@/types'

const api = vi.hoisted(() => ({ getActiveSubscriptions: vi.fn(), resetDailyQuota: vi.fn() }))
vi.mock('@/api/subscriptions', () => ({ default: api }))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) }))
vi.mock('@/utils/featureFlags', async () => ({
  ...await vi.importActual<typeof import('@/utils/featureFlags')>('@/utils/featureFlags'),
  isFeatureFlagEnabled: () => true
}))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'Sub2API' }))
vi.mock('vue-router', () => ({
  RouterView: { template: '<div />' },
  useRouter: () => ({ afterEach: vi.fn() }),
  useRoute: () => ({ path: '/', fullPath: '/', meta: {} })
}))
vi.mock('@/components/common/Toast.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/NavigationProgress.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/AnnouncementPopup.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/admin/AdminComplianceDialog.vue', () => ({ default: { template: '<div />' } }))

const auth = reactive({ isAuthenticated: true, isAdmin: false, user: { id: 7 } })
vi.mock('@/stores', () => ({
  useAuthStore: () => auth,
  useSubscriptionStore: () => useSubscriptionStore(),
  useAppStore: () => ({ siteName: 'Sub2API', fetchPublicSettings: vi.fn().mockResolvedValue(undefined) }),
  useAnnouncementStore: () => ({ fetchAnnouncements: vi.fn(), reset: vi.fn() }),
  useAdminComplianceStore: () => ({ reset: vi.fn() }),
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

function subscription(userID: number): UserSubscription {
  return {
    id: userID, user_id: userID, group_id: 1, status: 'active',
    starts_at: '2026-09-01T00:00:00Z', expires_at: '2026-10-01T00:00:00Z',
    created_at: '', updated_at: '', daily_usage_usd: 60, weekly_usage_usd: 60, monthly_usage_usd: 60,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null,
    daily_reset: {
      eligible: true, can_reset: true, auto_daily_reset_enabled: false,
      today_reset_count: 0, daily_reset_limit: 100, daily_reset_version: 1,
      server_date: '2026-09-17', server_time: '2026-09-17T00:00:00Z'
    }
  }
}

describe('subscription identity isolation', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionStorage.clear()
    vi.clearAllMocks()
    auth.isAuthenticated = true
    auth.user = { id: 7 }
    api.getActiveSubscriptions.mockImplementation(() => Promise.resolve([subscription(auth.user.id)]))
  })

  afterEach(() => { useSubscriptionStore().clear() })

  it('clears the previous identity before loading another authenticated user', async () => {
    const wrapper = mount(App)
    try {
      await flushPromises()
      const store = useSubscriptionStore()
      expect(store.activeSubscriptions[0].user_id).toBe(7)

      auth.user = { id: 8 }
      await flushPromises()

      expect(api.getActiveSubscriptions).toHaveBeenCalledTimes(2)
      expect(store.activeSubscriptions.map(item => item.user_id)).toEqual([8])
    } finally { wrapper.unmount() }
  })

  it('ignores an old paid reset response after the authenticated identity changes', async () => {
    let completeReset!: (value: unknown) => void
    api.resetDailyQuota.mockImplementation(() => new Promise(resolve => { completeReset = resolve }))
    const wrapper = mount(App)
    try {
      await flushPromises()
      const store = useSubscriptionStore()
      const oldReset = store.resetDailyQuota(subscription(7))
      auth.user = { id: 8 }
      await flushPromises()
      completeReset({ operation_id: 'old-reset', reset_performed: true, subscription: subscription(7) })
      await oldReset

      expect(store.subscriptions).toEqual([])
      expect(store.activeSubscriptions.map(item => item.user_id)).toEqual([8])
      expect(store.pendingDailyResets).toEqual({})
      expect(sessionStorage.getItem('subscription-daily-reset:7:7')).not.toBeNull()
    } finally { wrapper.unmount() }
  })

  it('keeps the cache when only the current user profile refreshes', async () => {
    const wrapper = mount(App)
    try {
      await flushPromises()
      auth.user = { id: 7 }
      await flushPromises()
      expect(api.getActiveSubscriptions).toHaveBeenCalledTimes(1)
      expect(useSubscriptionStore().activeSubscriptions[0].user_id).toBe(7)
    } finally { wrapper.unmount() }
  })
})
