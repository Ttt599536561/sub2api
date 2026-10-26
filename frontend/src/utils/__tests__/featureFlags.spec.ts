import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled, makeSidebarFlag, resolveFeatureFlag } from '@/utils/featureFlags'
import type { PublicSettings } from '@/types'

vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn(),
}))

describe('FeatureFlags.subscription', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    delete (window as any).__APP_CONFIG__
  })

  it('reads subscription_enabled as an opt-out flag: visible before settings load', () => {
    expect(FeatureFlags.subscription.key).toBe('subscription_enabled')
    expect(FeatureFlags.subscription.mode).toBe('opt-out')
    expect(useAppStore().cachedPublicSettings).toBeNull()
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(true)
  })

  it('hides only when the backend explicitly sends false', () => {
    const store = useAppStore()
    const sidebarFlag = makeSidebarFlag(FeatureFlags.subscription)

    store.cachedPublicSettings = { subscription_enabled: false } as PublicSettings
    expect(sidebarFlag()).toBe(false)

    store.cachedPublicSettings = { subscription_enabled: true } as PublicSettings
    expect(sidebarFlag()).toBe(true)

    store.cachedPublicSettings = {} as PublicSettings
    expect(sidebarFlag()).toBe(true)
  })
})

describe('resolveFeatureFlag', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('reads an explicit boolean from the given settings object', () => {
    expect(resolveFeatureFlag({ subscription_enabled: false } as PublicSettings, FeatureFlags.subscription)).toBe(false)
    expect(resolveFeatureFlag({ subscription_enabled: true } as PublicSettings, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({ available_channels_enabled: true } as PublicSettings, FeatureFlags.availableChannels)).toBe(true)
  })

  it('falls back to the declared mode when settings are missing or the key is absent', () => {
    expect(resolveFeatureFlag(undefined, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag(null, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({} as PublicSettings, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({} as PublicSettings, FeatureFlags.availableChannels)).toBe(false)
  })

  it('backs isFeatureFlagEnabled with the same resolution', () => {
    useAppStore().cachedPublicSettings = { subscription_enabled: false } as PublicSettings
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(false)
  })
})

describe('QQ business feature flags', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it.each([
    ['qqBinding', 'qq_binding_enabled'],
    ['qqImage', 'qq_image_enabled'],
  ] as const)('%s defaults to enabled for older public settings', (name, key) => {
    const flag = FeatureFlags[name]
    expect(flag).toMatchObject({ key, mode: 'opt-out' })
    expect(resolveFeatureFlag(null, flag)).toBe(true)
    expect(resolveFeatureFlag({}, flag)).toBe(true)
    expect(resolveFeatureFlag({ [key]: false }, flag)).toBe(false)
    expect(resolveFeatureFlag({ [key]: true }, flag)).toBe(true)
  })

  it('resolves binding and image generation independently', () => {
    const store = useAppStore()
    store.cachedPublicSettings = { qq_binding_enabled: false, qq_image_enabled: true } as PublicSettings
    expect(isFeatureFlagEnabled(FeatureFlags.qqBinding)).toBe(false)
    expect(isFeatureFlagEnabled(FeatureFlags.qqImage)).toBe(true)

    store.cachedPublicSettings = { qq_binding_enabled: true, qq_image_enabled: false } as PublicSettings
    expect(isFeatureFlagEnabled(FeatureFlags.qqBinding)).toBe(true)
    expect(isFeatureFlagEnabled(FeatureFlags.qqImage)).toBe(false)
  })
})
